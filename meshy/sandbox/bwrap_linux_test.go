//go:build linux

package sandbox

// Integration test: needs root, bwrap and /dev/fuse. Run via
//   make -C meshy test-linux   (docker, see deploy/Dockerfile.test)
// It is skipped elsewhere.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

type collector struct {
	mu sync.Mutex
	ev []audit.Event
}

func (c *collector) Emit(e audit.Event) { c.mu.Lock(); c.ev = append(c.ev, e); c.mu.Unlock() }
func (c *collector) has(kind audit.Kind, op, path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.ev {
		if e.Kind == kind && (op == "" || e.Op == op) && (path == "" || e.Path == path) {
			return true
		}
	}
	return false
}
func (c *collector) dump() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, e := range c.ev {
		b.WriteString(string(e.Kind) + " " + e.Op + " " + e.Path + " " + e.Reason + "\n")
	}
	return b.String()
}

func setupFS(t *testing.T) {
	t.Helper()
	for p, body := range map[string]string{
		"/srv/reports/q3.csv":      "revenue,42\n",
		"/srv/reports/drafts/a.md": "draft\n",
		"/srv/secrets/key":         "TOPSECRET\n",
	} {
		os.MkdirAll(p[:strings.LastIndex(p, "/")], 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink("/etc/shadow", "/srv/reports/evil-link")
	os.Symlink("/srv/secrets/key", "/srv/reports/evil-link2")
}

func run(t *testing.T, b *Bwrap, pol *policy.Policy, sink audit.Sink, script string) (string, int) {
	t.Helper()
	prep, err := b.Prepare(context.Background(), meshyd.SandboxSpec{
		SessionID: t.Name()[len(t.Name())-6:] + time.Now().Format("150405.000"),
		Policy:    pol, Argv: []string{"/bin/sh", "-c", script},
		Env: []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}, Audit: sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prep.Cleanup()
	var out bytes.Buffer
	prep.Cmd.Stdout, prep.Cmd.Stderr = &out, &out
	prep.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = prep.Cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let FUSE Release events land
	return out.String(), code
}

func requireEnv(t *testing.T) *Bwrap {
	if os.Geteuid() != 0 {
		t.Skip("needs root (run in docker)")
	}
	b := &Bwrap{Roots: []string{"/srv"}, RunDir: t.TempDir()}
	if err := b.Check(); err != nil {
		t.Skip("sandbox unavailable: ", err)
	}
	return b
}

func pol(t *testing.T, rules ...policy.PathRule) *policy.Policy {
	p := &policy.Policy{V: 1, Grant: "claude-1.db1.prod.acme.eth", Paths: rules}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBwrapAllowsGrantedRead(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	c := &collector{}
	out, code := run(t, b, pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly}), c, "cat /srv/reports/q3.csv")
	if code != 0 || !strings.Contains(out, "revenue,42") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !c.has(audit.Op, "read", "/srv/reports/q3.csv") {
		t.Fatalf("read not audited:\n%s", c.dump())
	}
}

func TestBwrapBlocksEscapes(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	p := pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly})
	cases := []struct {
		name, script, blockedPath string
	}{
		{"sibling dir", "cat /srv/secrets/key", "/srv/secrets"},
		{"dotdot", "cat /srv/reports/../secrets/key", "/srv/secrets"},
		{"etc shadow", "cat /etc/shadow", "/etc/shadow"},
		{"symlink to etc", "cat /srv/reports/evil-link", "/etc/shadow"},
		{"symlink to sibling", "cat /srv/reports/evil-link2", "/srv/secrets"},
		{"write in ro grant", "echo x > /srv/reports/new.txt", "/srv/reports/new.txt"},
		{"truncate in ro grant", ": > /srv/reports/q3.csv", "/srv/reports/q3.csv"},
		{"delete in ro grant", "rm -f /srv/reports/q3.csv", "/srv/reports/q3.csv"},
		{"list home", "ls /root", "/root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &collector{}
			out, code := run(t, b, p, c, tc.script)
			if code == 0 {
				t.Fatalf("escape succeeded: out=%q", out)
			}
			if strings.Contains(out, "TOPSECRET") {
				t.Fatalf("secret leaked: %q", out)
			}
			if !c.has(audit.Blocked, "", tc.blockedPath) {
				t.Fatalf("block for %s not audited:\n%s", tc.blockedPath, c.dump())
			}
		})
	}
	if data, _ := os.ReadFile("/srv/reports/q3.csv"); string(data) != "revenue,42\n" {
		t.Fatalf("ro file modified: %q", data)
	}
}

func TestBwrapListingHidesOutOfGrant(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	out, code := run(t, b, pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly}), &collector{}, "ls /srv")
	if code != 0 || strings.Contains(out, "secrets") || !strings.Contains(out, "reports") {
		t.Fatalf("code=%d ls /srv = %q", code, out)
	}
}

func TestBwrapWriteInRWGrant(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	c := &collector{}
	p := pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly}, policy.PathRule{Path: "/srv/reports/drafts", Mode: policy.ReadWrite})
	out, code := run(t, b, p, c, "echo hi > /srv/reports/drafts/b.md && cat /srv/reports/drafts/b.md")
	if code != 0 || strings.TrimSpace(out) != "hi" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !c.has(audit.Op, "create", "/srv/reports/drafts/b.md") {
		t.Fatalf("create not audited:\n%s", c.dump())
	}
}

func TestBwrapNoNetworkAndOwnPID(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	p := pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly})
	out, _ := run(t, b, p, &collector{}, "ls /sys/class/net 2>/dev/null; echo; cat /proc/1/cmdline | tr '\\0' ' '")
	if strings.Contains(out, "eth0") {
		t.Fatalf("sandbox has host network: %q", out)
	}
	if strings.Contains(out, "meshyd") || strings.Contains(out, ".test") {
		t.Fatalf("pid 1 is the host process: %q", out)
	}
}

func TestBwrapKillGroupKillsSandbox(t *testing.T) {
	b := requireEnv(t)
	setupFS(t)
	prep, err := b.Prepare(context.Background(), meshyd.SandboxSpec{
		SessionID: "killtest", Policy: pol(t, policy.PathRule{Path: "/srv/reports", Mode: policy.ReadOnly}),
		Argv: []string{"/bin/sh", "-c", "sleep 300 & sleep 300; wait"}, Env: []string{"PATH=/usr/bin:/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prep.Cleanup()
	prep.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := prep.Cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	syscall.Kill(-prep.Cmd.Process.Pid, syscall.SIGKILL)
	prep.Cmd.Wait()
	time.Sleep(200 * time.Millisecond)
	out, _ := exec.Command("pgrep", "-x", "sleep").Output()
	if len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("sandboxed processes survived kill: %s", out)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("kill took %s", time.Since(start))
	}
}
