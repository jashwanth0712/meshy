//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Seatbelt confines sessions on macOS with sandbox-exec (SBPL).
//
//   - deny default; system binaries/libraries readable; no network
//   - granted paths readable (ro) or readable+writable (rw)
//   - a private per-session TMPDIR is writable
//
// Blocked attempts are detected from the kernel's EPERM errors on the
// session's stderr (macOS does not expose sandbox denials to unprivileged
// readers). Allowed reads/writes are recorded by an optional eslogger
// Monitor (requires root + Full Disk Access).
type Seatbelt struct {
	SystemRead []string // extra readable subpaths (e.g. /opt/homebrew)
	RunDir     string   // per-session tmp dirs; default os.TempDir()/meshy
	Monitor    *ESMonitor
}

var seatbeltSystem = []string{
	"/usr", "/bin", "/sbin", "/System", "/Library", "/private/var/db", "/dev",
	"/private/etc", "/opt/homebrew", "/Applications/Xcode.app/Contents/Developer",
	"/Library/Developer/CommandLineTools",
}

func (s *Seatbelt) Name() string { return "sandbox-exec(seatbelt)" }

func (s *Seatbelt) Check() error {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		return fmt.Errorf("sandbox-exec not found: %w", err)
	}
	out, err := exec.Command("sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox-exec unusable: %s: %w", out, err)
	}
	return nil
}

func sbQuote(p string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
}

// realpath resolves symlinks (/tmp -> /private/tmp) since Seatbelt matches
// on vnode paths.
func realpath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Profile renders the SBPL profile for a policy.
func (s *Seatbelt) Profile(pol *policy.Policy, tmp string, session string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	fmt.Fprintf(&b, "(deny default (with message %s))\n", sbQuote("meshy:"+session))
	b.WriteString("(allow process-fork process-exec signal sysctl-read mach-lookup ipc-posix-shm process-info*)\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-read* (literal \"/\")")
	sys := append(append([]string{}, seatbeltSystem...), s.SystemRead...)
	sort.Strings(sys)
	for _, p := range sys {
		fmt.Fprintf(&b, " (subpath %s)", sbQuote(p))
	}
	b.WriteString(")\n")
	b.WriteString("(allow file-write* (literal \"/dev/null\") (literal \"/dev/tty\") (literal \"/dev/dtracehelper\"))\n")
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbQuote(realpath(tmp)))
	for _, r := range pol.Paths {
		rp := realpath(r.Path)
		if r.Mode == policy.ReadWrite {
			fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbQuote(rp))
		} else {
			fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", sbQuote(rp))
		}
		// Ancestors must be listable to reach the grant (names only, via
		// read-metadata above; directory listing of ancestors is not granted).
	}
	return b.String()
}

func (s *Seatbelt) Prepare(ctx context.Context, spec meshyd.SandboxSpec) (*meshyd.Prepared, error) {
	run := s.RunDir
	if run == "" {
		run = filepath.Join(os.TempDir(), "meshy")
	}
	tmp := filepath.Join(run, spec.SessionID)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	prof := s.Profile(spec.Policy, tmp, spec.SessionID)
	argv := append([]string{"-p", prof, "--"}, spec.Argv...)
	cmd := exec.Command("sandbox-exec", argv...)
	cmd.Env = append(spec.Env, "TMPDIR="+tmp+"/")
	cmd.Dir = workdirDarwin(spec.Policy, tmp)

	// eslogger and kernel errors report resolved paths (/private/tmp/...),
	// so match against a symlink-resolved copy of the policy.
	rpol := realPolicy(spec.Policy)
	var once sync.Once
	var stopMon func()
	var onStart func(int)
	if s.Monitor != nil {
		stopMon = s.Monitor.Track(spec.SessionID, cmd, rpol, spec.Audit)
		onStart = func(pid int) { s.Monitor.Attach(spec.SessionID, pid) }
	}
	cleanup := func() {
		once.Do(func() {
			if stopMon != nil {
				stopMon()
			}
			os.RemoveAll(tmp)
		})
	}
	return &meshyd.Prepared{Cmd: cmd, Cleanup: cleanup, OnStart: onStart, WrapStderr: func(w io.Writer) io.Writer {
		return &BlockTap{W: w, Policy: rpol, Audit: spec.Audit}
	}}, nil
}

func realPolicy(p *policy.Policy) *policy.Policy {
	cp := *p
	cp.Paths = nil
	for _, r := range p.Paths {
		cp.Paths = append(cp.Paths, policy.PathRule{Path: realpath(r.Path), Mode: r.Mode})
		if rp := realpath(r.Path); rp != r.Path { // keep the original spelling too
			cp.Paths = append(cp.Paths, r)
		}
	}
	return &cp
}

func workdirDarwin(pol *policy.Policy, tmp string) string {
	for _, r := range pol.Paths {
		if fi, err := os.Stat(r.Path); err == nil {
			if fi.IsDir() {
				return r.Path
			}
			return filepath.Dir(r.Path)
		}
	}
	return tmp
}

// BlockTap passes stderr through and audits EPERM/EACCES errors on paths
// as blocked attempts.
type BlockTap struct {
	W      io.Writer
	Policy *policy.Policy
	Audit  audit.Sink

	mu   sync.Mutex
	line []byte
	seen map[string]time.Time
}

var blockRe = []*regexp.Regexp{
	// cat: /etc/shadow: Operation not permitted   |   ls: /x: Permission denied
	regexp.MustCompile(`(?:^|: )(/[^:\n]+?): (?:Operation not permitted|Permission denied)`),
	// PermissionError: [Errno 1] Operation not permitted: '/x'
	regexp.MustCompile(`(?:Operation not permitted|Permission denied)[^'"\n]*['"](/[^'"\n]+)['"]`),
	// sh: /x: cannot create  /  can't open '/x'
	regexp.MustCompile(`(?:cannot (?:create|open)|can't (?:create|open))[^/\n]*(/[^:'"\n ]+)`),
}

func (t *BlockTap) Write(p []byte) (int, error) {
	n, err := t.W.Write(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.line = append(t.line, p...)
	for {
		i := strings.IndexByte(string(t.line), '\n')
		if i < 0 {
			if len(t.line) > 4096 {
				t.line = t.line[:0]
			}
			break
		}
		t.scan(string(t.line[:i]))
		t.line = t.line[i+1:]
	}
	return n, err
}

func (t *BlockTap) scan(line string) {
	for _, re := range blockRe {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := filepath.Clean(m[1])
		if t.seen == nil {
			t.seen = map[string]time.Time{}
		}
		if last, ok := t.seen[path]; ok && time.Since(last) < time.Second {
			return
		}
		t.seen[path] = time.Now()
		reason := "outside grant"
		if t.Policy.Check(path, false).Allowed {
			reason = "read-only grant"
		}
		if t.Audit != nil {
			t.Audit.Emit(audit.Event{Kind: audit.Blocked, TS: time.Now(), Op: "access", Path: path, Reason: reason})
		}
		return
	}
}
