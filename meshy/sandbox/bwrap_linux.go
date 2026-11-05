//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/jashwanth0712/meshy/meshy/meshyd"
)

// Bwrap confines sessions with bubblewrap + GateFS.
//
// Layout inside the sandbox:
//
//	/usr /bin /sbin /lib*     read-only host binds (symlinks preserved for merged-usr)
//	/proc /dev /tmp /var      private (new pid ns, minimal dev, tmpfs)
//	/etc /home /root <roots>  GateFS: grant + system allow-list, audited
//	network                   none (unshared netns)
//
// meshyd needs /dev/fuse and CAP_SYS_ADMIN; in Docker run with
// --device /dev/fuse --cap-add SYS_ADMIN --security-opt seccomp=unconfined
// --security-opt apparmor=unconfined.
type Bwrap struct {
	Path       string   // bwrap binary; default "bwrap"
	Roots      []string // data roots to gate, e.g. ["/srv"]; must exist
	SystemRead []string // default DefaultSystemRead
	RunDir     string   // where FUSE mountpoints live; default /run/meshy
	UID, GID   int      // in-sandbox identity; default 1000
}

var alwaysGated = []string{"/etc", "/home", "/root"}

func (b *Bwrap) Name() string { return "bwrap+gatefs" }

func (b *Bwrap) bin() string {
	if b.Path != "" {
		return b.Path
	}
	return "bwrap"
}

// Check verifies bwrap, /dev/fuse, and that namespaces can be created.
func (b *Bwrap) Check() error {
	if _, err := exec.LookPath(b.bin()); err != nil {
		return fmt.Errorf("bwrap not found: %w (apt install bubblewrap)", err)
	}
	if _, err := os.Stat("/dev/fuse"); err != nil {
		return fmt.Errorf("/dev/fuse missing: run the container with --device /dev/fuse: %w", err)
	}
	out, err := exec.Command(b.bin(), "--unshare-all", "--die-with-parent", "--ro-bind", "/", "/", "true").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bwrap cannot create namespaces (%s): %w; in Docker add --security-opt seccomp=unconfined --security-opt apparmor=unconfined", out, err)
	}
	for _, r := range b.Roots {
		if fi, err := os.Stat(r); err != nil || !fi.IsDir() {
			return fmt.Errorf("data root %s is not a directory", r)
		}
	}
	return nil
}

func (b *Bwrap) gates() []Gate {
	seen := map[string]bool{}
	var gs []Gate
	for _, p := range append(append([]string{}, alwaysGated...), b.Roots...) {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		seen[p] = true
		gs = append(gs, Gate{Source: p, Target: p})
	}
	return gs
}

func (b *Bwrap) Prepare(ctx context.Context, spec meshyd.SandboxSpec) (*meshyd.Prepared, error) {
	run := b.RunDir
	if run == "" {
		run = "/run/meshy"
	}
	dir := filepath.Join(run, spec.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sys := b.SystemRead
	if sys == nil {
		sys = DefaultSystemRead
	}
	rules := &Rules{Policy: spec.Policy, SystemRead: sys, Audit: spec.Audit}

	var servers []*fuse.Server
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			for _, s := range servers {
				_ = s.Unmount()
			}
			_ = os.RemoveAll(dir)
		})
	}

	uid, gid := b.UID, b.GID
	if uid == 0 {
		uid, gid = 1000, 1000
	}
	args := []string{
		"--die-with-parent", "--new-session", "--unshare-all",
		"--uid", fmt.Sprint(uid), "--gid", fmt.Sprint(gid),
		"--cap-drop", "ALL", "--hostname", "meshy-sandbox",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/var",
		"--clearenv",
	}
	for _, d := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32"} {
		fi, err := os.Lstat(d)
		switch {
		case err != nil:
			continue
		case fi.Mode()&os.ModeSymlink != 0:
			t, err := os.Readlink(d)
			if err == nil {
				args = append(args, "--symlink", t, d)
			}
		case fi.IsDir():
			args = append(args, "--ro-bind", d, d)
		}
	}
	for i, g := range b.gates() {
		mp := filepath.Join(dir, fmt.Sprintf("g%d", i))
		if err := os.MkdirAll(mp, 0o755); err != nil {
			cleanup()
			return nil, err
		}
		srv, err := MountGate(g, mp, rules)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("mount gate %s: %w", g.Target, err)
		}
		servers = append(servers, srv)
		args = append(args, "--bind", mp, g.Target)
	}
	if wd := workdir(spec); wd != "" {
		args = append(args, "--chdir", wd)
	}
	for _, kv := range spec.Env {
		k, v, _ := cut(kv)
		args = append(args, "--setenv", k, v)
	}
	args = append(args, "--")
	args = append(args, spec.Argv...)

	cmd := exec.Command(b.bin(), args...)
	return &meshyd.Prepared{Cmd: cmd, Cleanup: cleanup}, nil
}

func workdir(spec meshyd.SandboxSpec) string {
	for _, r := range spec.Policy.Paths {
		if fi, err := os.Stat(r.Path); err == nil {
			if fi.IsDir() {
				return r.Path
			}
			return filepath.Dir(r.Path)
		}
	}
	return "/tmp"
}

func cut(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}

// ErrUnsupported is returned by sandboxes that don't apply to this OS.
var ErrUnsupported = errors.New("sandbox: unsupported on this OS")
