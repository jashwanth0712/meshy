//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

type col struct {
	mu sync.Mutex
	ev []audit.Event
}

func (c *col) Emit(e audit.Event) { c.mu.Lock(); c.ev = append(c.ev, e); c.mu.Unlock() }
func (c *col) blocked(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.ev {
		if e.Kind == audit.Blocked && strings.HasSuffix(e.Path, path) {
			return true
		}
	}
	return false
}

func fixture(t *testing.T) (root string) {
	root = realpath(t.TempDir())
	for p, body := range map[string]string{
		"reports/q3.csv":    "revenue,42\n",
		"reports/drafts/.k": "",
		"secrets/key":       "TOPSECRET\n",
	} {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	os.Symlink(filepath.Join(root, "secrets/key"), filepath.Join(root, "reports/link"))
	return root
}

func runSB(t *testing.T, pol *policy.Policy, script string) (string, int, *col) {
	t.Helper()
	sb := &Seatbelt{RunDir: t.TempDir()}
	if err := sb.Check(); err != nil {
		t.Skip(err)
	}
	c := &col{}
	prep, err := sb.Prepare(context.Background(), meshyd.SandboxSpec{
		SessionID: "t", Policy: pol, Argv: []string{"/bin/sh", "-c", script},
		Env: []string{"PATH=/usr/bin:/bin"}, Audit: c,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prep.Cleanup()
	var out, errb bytes.Buffer
	prep.Cmd.Stdout = &out
	prep.Cmd.Stderr = prep.WrapStderr(&errb)
	prep.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = prep.Cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return out.String() + errb.String(), code, c
}

func mkpol(t *testing.T, rules ...policy.PathRule) *policy.Policy {
	p := &policy.Policy{V: 1, Grant: "claude-1.db1.prod.acme.eth", Paths: rules}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSeatbeltAllowsGrant(t *testing.T) {
	root := fixture(t)
	out, code, _ := runSB(t, mkpol(t, policy.PathRule{Path: root + "/reports", Mode: policy.ReadOnly}), "cat "+root+"/reports/q3.csv")
	if code != 0 || !strings.Contains(out, "revenue,42") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestSeatbeltBlocksAndAudits(t *testing.T) {
	root := fixture(t)
	p := mkpol(t, policy.PathRule{Path: root + "/reports", Mode: policy.ReadOnly}, policy.PathRule{Path: root + "/reports/drafts", Mode: policy.ReadWrite})
	cases := []struct{ name, script, path string }{
		{"sibling", "cat " + root + "/secrets/key", "/secrets/key"},
		{"dotdot", "cat " + root + "/reports/../secrets/key", "/secrets/key"},
		{"symlink", "cat " + root + "/reports/link", "/reports/link"},
		{"write ro", "echo x > " + root + "/reports/new.txt", "/reports/new.txt"},
	}
	// A real file in the user's home (outside any grant), if one exists.
	if home, err := os.UserHomeDir(); err == nil {
		for _, f := range []string{".zshrc", ".gitconfig", ".bash_profile", ".profile"} {
			if _, err := os.Stat(filepath.Join(home, f)); err == nil {
				cases = append(cases, struct{ name, script, path string }{"home file", "cat " + filepath.Join(home, f), "/" + f})
				break
			}
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code, c := runSB(t, p, tc.script)
			if code == 0 || strings.Contains(out, "TOPSECRET") {
				t.Fatalf("escape: code=%d out=%q", code, out)
			}
			if !c.blocked(tc.path) {
				t.Fatalf("block not audited for %s; out=%q events=%+v", tc.path, out, c.ev)
			}
		})
	}
	out, code, _ := runSB(t, p, "echo hi > "+root+"/reports/drafts/b.md && cat "+root+"/reports/drafts/b.md")
	if code != 0 || strings.TrimSpace(out) != "hi" {
		t.Fatalf("rw grant: code=%d out=%q", code, out)
	}
}

func TestSeatbeltNoNetwork(t *testing.T) {
	root := fixture(t)
	out, code, _ := runSB(t, mkpol(t, policy.PathRule{Path: root, Mode: policy.ReadOnly}), "/usr/bin/curl -sS -m 3 https://example.com -o /dev/null")
	if code == 0 {
		t.Fatalf("network reachable from sandbox: %q", out)
	}
}

func TestESMonitorParsesForkAndOpen(t *testing.T) {
	c := &col{}
	pol := mkpol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly})
	m := &ESMonitor{}
	stop := m.Track("s1", nil, pol, c)
	defer stop()
	m.Attach("s1", 100)
	m.handle([]byte(`{"process":{"audit_token":{"pid":100}},"event":{"fork":{"child":{"audit_token":{"pid":101}}}}}`))
	m.handle([]byte(`{"process":{"audit_token":{"pid":101}},"event":{"open":{"file":{"path":"/srv/reports/q3.csv"},"fflag":1}}}`))
	m.handle([]byte(`{"process":{"audit_token":{"pid":101}},"event":{"open":{"file":{"path":"/usr/lib/libSystem.B.dylib"}}}}`))
	m.handle([]byte(`{"process":{"audit_token":{"pid":999}},"event":{"open":{"file":{"path":"/srv/reports/q3.csv"}}}}`))
	if len(c.ev) != 1 || c.ev[0].Path != "/srv/reports/q3.csv" || c.ev[0].Op != "open" {
		t.Fatalf("events = %+v", c.ev)
	}
}
