// Package sandbox confines agent sessions. On Linux, GateFS (a policy-checking
// FUSE loopback) is mounted over every gated root (data roots like /srv plus
// sensitive dirs like /etc, /home, /root) inside a bubblewrap namespace.
// Every lookup, open, read, write and mutation is checked against the grant's
// policy in meshyd; denials return EACCES and are audited as blocks.
//
// Because each gate sees the absolute in-sandbox path, symlink tricks
// (reports/x -> /etc/shadow) re-enter the gate of the target and are denied
// there.
package sandbox

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Rules decides access for absolute in-sandbox paths.
type Rules struct {
	Policy *policy.Policy
	// SystemRead lists paths readable regardless of the grant (e.g. files
	// under /etc that libc and tools need). Prefix-matched on components.
	SystemRead []string
	Audit      audit.Sink
}

// DefaultSystemRead is what common CLI tools need from /etc.
var DefaultSystemRead = []string{
	"/etc/passwd", "/etc/group", "/etc/hosts", "/etc/hostname", "/etc/resolv.conf",
	"/etc/nsswitch.conf", "/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d",
	"/etc/localtime", "/etc/timezone", "/etc/ssl", "/etc/ca-certificates", "/etc/alternatives",
	"/etc/os-release", "/etc/mime.types", "/etc/protocols", "/etc/services", "/etc/inputrc",
	"/etc/terminfo", "/etc/profile", "/etc/profile.d", "/etc/bash.bashrc", "/etc/environment",
	"/etc/locale.alias", "/etc/gai.conf", "/etc/host.conf", "/etc/python3", "/etc/gitconfig",
}

func within(base, p string) bool { return p == base || strings.HasPrefix(p, base+"/") }

// visible reports whether path may be looked up / listed (read access, or an
// ancestor directory on the way to something readable).
func (r *Rules) visible(p string) bool {
	if r.readable(p) {
		return true
	}
	for _, rule := range r.Policy.Paths {
		if within(p, rule.Path) { // p is an ancestor of a granted path
			return true
		}
	}
	for _, s := range r.SystemRead {
		if within(p, s) {
			return true
		}
	}
	return false
}

func (r *Rules) readable(p string) bool {
	if r.Policy.Check(p, false).Allowed {
		return true
	}
	for _, s := range r.SystemRead {
		if within(s, p) {
			return true
		}
	}
	return false
}

func (r *Rules) writable(p string) bool { return r.Policy.Check(p, true).Allowed }

func (r *Rules) emit(kind audit.Kind, op, path, reason string, n int64) {
	if r.Audit != nil {
		r.Audit.Emit(audit.Event{Kind: kind, TS: time.Now(), Op: op, Path: path, Reason: reason, Bytes: n})
	}
}

func (r *Rules) block(op, path string) syscall.Errno {
	reason := "outside grant"
	if r.readable(path) {
		reason = "read-only grant"
	}
	r.emit(audit.Blocked, op, path, reason, 0)
	return syscall.EACCES
}

// isSystem reports whether p is only readable via the system allow-list (not
// the grant). System reads are not audited, to keep the log about the data.
func (r *Rules) isSystem(p string) bool { return !r.Policy.Check(p, false).Allowed }

// Gate is one gated root.
type Gate struct {
	// Source is the host directory; Target is where it appears in the
	// sandbox. They are normally equal (we gate /srv onto /srv).
	Source, Target string
}

// gateNode wraps a loopback node with policy checks.
type gateNode struct {
	*fs.LoopbackNode
	gate  *Gate
	rules *Rules
}

var (
	_ fs.NodeWrapChilder     = (*gateNode)(nil)
	_ fs.NodeLookuper        = (*gateNode)(nil)
	_ fs.NodeOpener          = (*gateNode)(nil)
	_ fs.NodeOpendirHandler  = (*gateNode)(nil)
	_ fs.NodeReaddirer       = (*gateNode)(nil)
	_ fs.NodeCreater         = (*gateNode)(nil)
	_ fs.NodeMkdirer         = (*gateNode)(nil)
	_ fs.NodeMknoder         = (*gateNode)(nil)
	_ fs.NodeUnlinker        = (*gateNode)(nil)
	_ fs.NodeRmdirer         = (*gateNode)(nil)
	_ fs.NodeRenamer         = (*gateNode)(nil)
	_ fs.NodeSymlinker       = (*gateNode)(nil)
	_ fs.NodeLinker          = (*gateNode)(nil)
	_ fs.NodeSetattrer       = (*gateNode)(nil)
	_ fs.NodeSetxattrer      = (*gateNode)(nil)
	_ fs.NodeRemovexattrer   = (*gateNode)(nil)
	_ fs.NodeCopyFileRanger  = (*gateNode)(nil)
)

func (n *gateNode) WrapChild(ctx context.Context, ops fs.InodeEmbedder) fs.InodeEmbedder {
	return &gateNode{LoopbackNode: ops.(*fs.LoopbackNode), gate: n.gate, rules: n.rules}
}

// vpath is this node's absolute path inside the sandbox.
func (n *gateNode) vpath() string {
	rel := n.Path(n.RootData.RootNode.EmbeddedInode())
	return filepath.Join(n.gate.Target, rel)
}

func (n *gateNode) child(name string) string { return filepath.Join(n.vpath(), name) }

func (n *gateNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := n.child(name)
	if !n.rules.visible(p) {
		// Only report a block if the file exists; probing for missing names
		// (shells, PATH lookups) would otherwise flood the log.
		var st syscall.Stat_t
		if syscall.Lstat(filepath.Join(n.gate.Source, strings.TrimPrefix(p, n.gate.Target)), &st) != nil {
			return nil, syscall.ENOENT
		}
		return nil, n.rules.block("lookup", p)
	}
	return n.LoopbackNode.Lookup(ctx, name, out)
}

func (n *gateNode) entries() ([]fuse.DirEntry, syscall.Errno) {
	ds, errno := fs.NewLoopbackDirStream(filepath.Join(n.RootData.Path, n.Path(n.RootData.RootNode.EmbeddedInode())))
	if errno != 0 {
		return nil, errno
	}
	defer ds.Close()
	var out []fuse.DirEntry
	for ds.HasNext() {
		e, errno := ds.Next()
		if errno != 0 {
			return nil, errno
		}
		if e.Name == "." || e.Name == ".." || n.rules.visible(n.child(e.Name)) {
			out = append(out, e)
		}
	}
	return out, 0
}

func (n *gateNode) OpendirHandle(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	p := n.vpath()
	if !n.rules.visible(p) {
		return nil, 0, n.rules.block("readdir", p)
	}
	list, errno := n.entries()
	if errno != 0 {
		return nil, 0, errno
	}
	if !n.rules.isSystem(p) && n.rules.readable(p) {
		n.rules.emit(audit.Op, "readdir", p, "", 0)
	}
	return fs.NewListDirStream(list), 0, 0
}

func (n *gateNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	list, errno := n.entries()
	if errno != 0 {
		return nil, errno
	}
	return fs.NewListDirStream(list), 0
}

const writeFlags = syscall.O_WRONLY | syscall.O_RDWR | syscall.O_TRUNC | syscall.O_APPEND | syscall.O_CREAT

func (n *gateNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	p := n.vpath()
	write := flags&writeFlags != 0
	switch {
	case write && !n.rules.writable(p):
		return nil, 0, n.rules.block("open-write", p)
	case !write && !n.rules.readable(p):
		return nil, 0, n.rules.block("open", p)
	}
	fh, ff, errno := n.LoopbackNode.Open(ctx, flags)
	if errno != 0 {
		return nil, 0, errno
	}
	if n.rules.isSystem(p) {
		return fh, ff, 0
	}
	return &auditFile{FileHandle: fh, path: p, rules: n.rules}, ff, 0
}

func (n *gateNode) Create(ctx context.Context, name string, flags, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	p := n.child(name)
	if !n.rules.writable(p) {
		return nil, nil, 0, n.rules.block("create", p)
	}
	in, fh, ff, errno := n.LoopbackNode.Create(ctx, name, flags, mode, out)
	if errno != 0 {
		return nil, nil, 0, errno
	}
	n.rules.emit(audit.Op, "create", p, "", 0)
	return in, &auditFile{FileHandle: fh, path: p, rules: n.rules}, ff, 0
}

func (n *gateNode) mutate(op, p string, f func() syscall.Errno) syscall.Errno {
	if !n.rules.writable(p) {
		return n.rules.block(op, p)
	}
	errno := f()
	if errno == 0 {
		n.rules.emit(audit.Op, op, p, "", 0)
	}
	return errno
}

func (n *gateNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	var in *fs.Inode
	errno := n.mutate("mkdir", n.child(name), func() (e syscall.Errno) { in, e = n.LoopbackNode.Mkdir(ctx, name, mode, out); return })
	return in, errno
}

func (n *gateNode) Mknod(ctx context.Context, name string, mode, rdev uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, n.rules.block("mknod", n.child(name)) // never needed by agents
}

func (n *gateNode) Unlink(ctx context.Context, name string) syscall.Errno {
	return n.mutate("unlink", n.child(name), func() syscall.Errno { return n.LoopbackNode.Unlink(ctx, name) })
}

func (n *gateNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	return n.mutate("rmdir", n.child(name), func() syscall.Errno { return n.LoopbackNode.Rmdir(ctx, name) })
}

func (n *gateNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	from := n.child(name)
	np, ok := newParent.(*gateNode)
	if !ok {
		return syscall.EXDEV
	}
	to := np.child(newName)
	if !n.rules.writable(from) {
		return n.rules.block("rename", from)
	}
	if !n.rules.writable(to) {
		return n.rules.block("rename", to)
	}
	errno := n.LoopbackNode.Rename(ctx, name, np.LoopbackNode, newName, flags)
	if errno == 0 {
		n.rules.emit(audit.Op, "rename", from+" -> "+to, "", 0)
	}
	return errno
}

func (n *gateNode) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	var in *fs.Inode
	errno := n.mutate("symlink", n.child(name), func() (e syscall.Errno) { in, e = n.LoopbackNode.Symlink(ctx, target, name, out); return })
	return in, errno
}

func (n *gateNode) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	t, ok := target.(*gateNode)
	if !ok || !n.rules.writable(t.vpath()) {
		// Hard-linking needs write access to the source too, so a link cannot
		// smuggle a read-only (or out-of-grant) inode into a writable dir.
		return nil, n.rules.block("link", n.child(name))
	}
	var in *fs.Inode
	errno := n.mutate("link", n.child(name), func() (e syscall.Errno) { in, e = n.LoopbackNode.Link(ctx, t.LoopbackNode, name, out); return })
	return in, errno
}

func (n *gateNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if af, ok := f.(*auditFile); ok {
		f = af.FileHandle
	}
	return n.mutate("setattr", n.vpath(), func() syscall.Errno { return n.LoopbackNode.Setattr(ctx, f, in, out) })
}

func (n *gateNode) Setxattr(ctx context.Context, attr string, data []byte, flags uint32) syscall.Errno {
	return n.mutate("setxattr", n.vpath(), func() syscall.Errno { return n.LoopbackNode.Setxattr(ctx, attr, data, flags) })
}

func (n *gateNode) Removexattr(ctx context.Context, attr string) syscall.Errno {
	return n.mutate("removexattr", n.vpath(), func() syscall.Errno { return n.LoopbackNode.Removexattr(ctx, attr) })
}

func (n *gateNode) CopyFileRange(ctx context.Context, fhIn fs.FileHandle, offIn uint64, out *fs.Inode, fhOut fs.FileHandle, offOut, length uint64, flags uint64) (uint32, syscall.Errno) {
	return 0, syscall.ENOTSUP // force a read+write fallback through audited handles
}

// auditFile wraps a loopback file handle and records the bytes moved.
type auditFile struct {
	fs.FileHandle
	path    string
	rules   *Rules
	read    atomic.Int64
	written atomic.Int64
	done    atomic.Bool
}

var (
	_ fs.FileReader   = (*auditFile)(nil)
	_ fs.FileWriter   = (*auditFile)(nil)
	_ fs.FileReleaser = (*auditFile)(nil)
	_ fs.FileFlusher  = (*auditFile)(nil)
	_ fs.FileFsyncer  = (*auditFile)(nil)
	_ fs.FileGetattrer = (*auditFile)(nil)
	_ fs.FileLseeker  = (*auditFile)(nil)
)

func (f *auditFile) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	res, errno := f.FileHandle.(fs.FileReader).Read(ctx, dest, off)
	if errno == 0 && res != nil {
		f.read.Add(int64(res.Size()))
	}
	return res, errno
}

func (f *auditFile) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	n, errno := f.FileHandle.(fs.FileWriter).Write(ctx, data, off)
	f.written.Add(int64(n))
	return n, errno
}

func (f *auditFile) Flush(ctx context.Context) syscall.Errno {
	if fl, ok := f.FileHandle.(fs.FileFlusher); ok {
		return fl.Flush(ctx)
	}
	return 0
}

func (f *auditFile) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	if fl, ok := f.FileHandle.(fs.FileFsyncer); ok {
		return fl.Fsync(ctx, flags)
	}
	return 0
}

func (f *auditFile) Getattr(ctx context.Context, out *fuse.AttrOut) syscall.Errno {
	if g, ok := f.FileHandle.(fs.FileGetattrer); ok {
		return g.Getattr(ctx, out)
	}
	return syscall.ENOTSUP
}

func (f *auditFile) Lseek(ctx context.Context, off uint64, whence uint32) (uint64, syscall.Errno) {
	if l, ok := f.FileHandle.(fs.FileLseeker); ok {
		return l.Lseek(ctx, off, whence)
	}
	return 0, syscall.ENOTSUP
}

// Release emits one op per file handle (not per read) to keep Arkiv volume
// sane: "read 4096 bytes of /srv/reports/q3.csv".
func (f *auditFile) Release(ctx context.Context) syscall.Errno {
	if f.done.CompareAndSwap(false, true) {
		r, w := f.read.Load(), f.written.Load()
		switch {
		case w > 0:
			f.rules.emit(audit.Op, "write", f.path, "", w)
		case r > 0:
			f.rules.emit(audit.Op, "read", f.path, "", r)
		default:
			f.rules.emit(audit.Op, "open", f.path, "", 0)
		}
	}
	if rl, ok := f.FileHandle.(fs.FileReleaser); ok {
		return rl.Release(ctx)
	}
	return 0
}

// MountGate mounts a GateFS for g at mountpoint and returns its server.
func MountGate(g Gate, mountpoint string, rules *Rules) (*fuse.Server, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(g.Source, &st); err != nil {
		return nil, err
	}
	root := &fs.LoopbackRoot{Path: g.Source, Dev: uint64(st.Dev)}
	node := &gateNode{LoopbackNode: &fs.LoopbackNode{RootData: root}, gate: &g, rules: rules}
	root.RootNode = node
	sec := time.Second
	return fs.Mount(mountpoint, node, &fs.Options{
		AttrTimeout:     &sec,
		EntryTimeout:    &sec,
		NegativeTimeout: &sec,
		MountOptions: fuse.MountOptions{
			AllowOther: true,
			FsName:     "meshy-gate:" + g.Target,
			Name:       "meshygate",
			// No kernel-level permission bypass: we enforce in user space.
			DisableXAttrs: false,
		},
	})
}
