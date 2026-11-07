// Package davfs serves a device's granted folders as a WebDAV share, so a
// Mac mounts them in Finder like any network volume (Go › Connect to Server).
//
// The files come over SFTP from the device's meshyd, through the same
// sandboxed session as `meshy ssh`: the device's folder policy decides what
// can be read or written, every block is audited there, and the volume dies
// with the grant. Nothing here is a security boundary.
//
// Layout: the share's root lists the device's shared folders by their last
// path element (/Users/jash/Documents/oct26 → /oct26), and only the ones this
// grant can open. Finder's own metadata (._ AppleDouble files, .DS_Store) is
// kept in memory on this Mac instead of being written to the device.
package davfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/net/webdav"
)

// SFTPCommand is run as the session command on the device: the system
// sftp-server, inside meshyd's sandbox, so the device's folder policy (and its
// audit) applies to every file the volume touches.
const SFTPCommand = `for p in /usr/libexec/sftp-server /usr/lib/openssh/sftp-server /usr/lib/ssh/sftp-server /usr/libexec/openssh/sftp-server; do [ -x "$p" ] && exec "$p" -e; done; echo "MESHY_NO_SFTP the device has no sftp-server" >&2; exit 127`

// Remote is the subset of *sftp.Client the share uses (an interface for tests).
type Remote interface {
	Stat(p string) (os.FileInfo, error)
	ReadDir(p string) ([]os.FileInfo, error)
	OpenFile(p string, flags int) (*sftp.File, error)
	Mkdir(p string) error
	RemoveAll(p string) error
	PosixRename(oldname, newname string) error
	Rename(oldname, newname string) error
}

// FS is a webdav.FileSystem over a device's granted folders.
type FS struct {
	r     Remote
	roots map[string]string // share name -> absolute path on the device
	names []string
	meta  webdav.FileSystem // Finder metadata, in memory
	mu    sync.Mutex
	cache statCache
}

// New returns a share of roots (absolute paths on the device). Roots that
// can't be opened with this grant are left out; New fails if none can.
func New(r Remote, roots []string) (*FS, error) {
	f := &FS{r: r, roots: map[string]string{}, meta: webdav.NewMemFS()}
	for _, root := range roots {
		root = path.Clean(root)
		if !path.IsAbs(root) || root == "/" {
			continue
		}
		fi, err := r.Stat(root)
		if err != nil || !fi.IsDir() {
			continue
		}
		if _, err := r.ReadDir(root); err != nil {
			continue // shared by the device, but not granted to us
		}
		name := path.Base(root)
		for i := 2; f.roots[name] != ""; i++ {
			name = path.Base(root) + "-" + itoa(i)
		}
		f.roots[name] = root
		f.names = append(f.names, name)
	}
	if len(f.names) == 0 {
		return nil, errors.New("MESHY_NO_ACCESS none of the device's shared folders can be opened with this grant")
	}
	sort.Strings(f.names)
	return f, nil
}

// Roots maps share names to device paths.
func (f *FS) Roots() map[string]string { return f.roots }

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// IsFinderMeta reports Finder's own bookkeeping files, which stay on this Mac.
func IsFinderMeta(name string) bool {
	b := path.Base(name)
	return strings.HasPrefix(b, "._") || b == ".DS_Store" || b == ".localized" ||
		b == ".Spotlight-V100" || b == ".Trashes" || b == ".fseventsd" || b == ".metadata_never_index" ||
		b == ".hidden" || b == ".ql_disablethumbnails" || b == ".ql_disablecache"
}

// resolve maps a share path to (device path, ok). "/" is the virtual root.
func (f *FS) resolve(name string) (string, bool, error) {
	name = path.Clean("/" + name)
	if name == "/" {
		return "", true, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(name, "/"), "/", 2)
	root, ok := f.roots[parts[0]]
	if !ok {
		return "", false, os.ErrNotExist
	}
	if len(parts) == 1 {
		return root, false, nil
	}
	return path.Join(root, parts[1]), false, nil
}

func (f *FS) metaDirs(name string) {
	dir := path.Dir(path.Clean("/" + name))
	var acc string
	for _, p := range strings.Split(strings.TrimPrefix(dir, "/"), "/") {
		if p == "" {
			continue
		}
		acc += "/" + p
		_ = f.meta.Mkdir(context.Background(), acc, 0o755)
	}
}

func (f *FS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if IsFinderMeta(name) {
		f.metaDirs(name)
		return f.meta.Mkdir(ctx, name, perm)
	}
	p, root, err := f.resolve(name)
	if err != nil {
		return err
	}
	if root || isTop(name) {
		return os.ErrPermission // the share's folders are the device's roots
	}
	f.cache.drop(p)
	return f.r.Mkdir(p)
}

func isTop(name string) bool { return path.Dir(path.Clean("/"+name)) == "/" }

func (f *FS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if IsFinderMeta(name) {
		f.mu.Lock()
		f.metaDirs(name)
		f.mu.Unlock()
		return f.meta.OpenFile(ctx, name, flag, perm)
	}
	p, root, err := f.resolve(name)
	if err != nil {
		return nil, err
	}
	if root {
		return &rootDir{f: f}, nil
	}
	writing := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
	if !writing {
		fi, err := f.remoteStat(p)
		if err != nil {
			return nil, err
		}
		if fi.IsDir() {
			return &remoteDir{f: f, p: p, fi: renamed(fi, path.Base(name))}, nil
		}
		// Nothing goes over SFTP until Finder actually reads (listings open every file).
		return &lazyFile{f: f, p: p, name: path.Base(name), fi: fi}, nil
	} else if isTop(name) {
		return nil, os.ErrPermission
	}
	f.cache.drop(p)
	h, err := f.r.OpenFile(p, flag)
	if err != nil {
		return nil, err
	}
	return &remoteFile{File: h, name: path.Base(name), f: f, p: p}, nil
}

func (f *FS) RemoveAll(ctx context.Context, name string) error {
	if IsFinderMeta(name) {
		return f.meta.RemoveAll(ctx, name)
	}
	p, root, err := f.resolve(name)
	if err != nil {
		return err
	}
	if root || isTop(name) {
		return os.ErrPermission
	}
	f.cache.drop(p)
	return f.r.RemoveAll(p)
}

func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	if IsFinderMeta(oldName) || IsFinderMeta(newName) {
		if IsFinderMeta(oldName) && IsFinderMeta(newName) {
			f.metaDirs(newName)
			return f.meta.Rename(ctx, oldName, newName)
		}
		return os.ErrPermission
	}
	op, oroot, err := f.resolve(oldName)
	if err != nil {
		return err
	}
	np, nroot, err := f.resolve(newName)
	if err != nil {
		return err
	}
	if oroot || nroot || isTop(oldName) || isTop(newName) {
		return os.ErrPermission
	}
	f.cache.drop(op)
	f.cache.drop(np)
	if err := f.r.PosixRename(op, np); err != nil {
		return f.r.Rename(op, np)
	}
	return nil
}

func (f *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if IsFinderMeta(name) {
		return f.meta.Stat(ctx, name)
	}
	p, root, err := f.resolve(name)
	if err != nil {
		return nil, err
	}
	if root {
		return dirInfo("/"), nil
	}
	fi, err := f.remoteStat(p)
	if err != nil {
		return nil, err
	}
	return renamed(fi, path.Base(path.Clean("/"+name))), nil
}

// ---- files and directories ----

type remoteFile struct {
	*sftp.File
	name string
	f    *FS
	p    string
}

// Close forgets the file's cached stat: its size and time just changed.
func (r *remoteFile) Close() error {
	err := r.File.Close()
	r.f.cache.drop(r.p)
	return err
}

func (r *remoteFile) Readdir(int) ([]fs.FileInfo, error) { return nil, os.ErrInvalid }
func (r *remoteFile) Stat() (fs.FileInfo, error) {
	fi, err := r.File.Stat()
	if err != nil {
		return nil, err
	}
	return renamed(fi, r.name), nil
}

type remoteDir struct {
	f    *FS
	p    string
	fi   os.FileInfo
	list []os.FileInfo
	pos  int
	read bool
}

func (d *remoteDir) Close() error                   { return nil }
func (d *remoteDir) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (d *remoteDir) Write([]byte) (int, error)      { return 0, os.ErrInvalid }
func (d *remoteDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *remoteDir) Stat() (fs.FileInfo, error)     { return d.fi, nil }
func (d *remoteDir) Readdir(count int) ([]fs.FileInfo, error) {
	if !d.read {
		l, err := d.f.remoteList(d.p)
		if err != nil {
			return nil, err
		}
		for _, fi := range l {
			if !IsFinderMeta(fi.Name()) {
				d.list = append(d.list, fi)
			}
		}
		d.read = true
	}
	return page(d.list, &d.pos, count)
}

type rootDir struct {
	f   *FS
	pos int
}

func (d *rootDir) Close() error                   { return nil }
func (d *rootDir) Read([]byte) (int, error)       { return 0, os.ErrInvalid }
func (d *rootDir) Write([]byte) (int, error)      { return 0, os.ErrInvalid }
func (d *rootDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *rootDir) Stat() (fs.FileInfo, error)     { return dirInfo("/"), nil }
func (d *rootDir) Readdir(count int) ([]fs.FileInfo, error) {
	var l []fs.FileInfo
	for _, n := range d.f.names {
		if fi, err := d.f.remoteStat(d.f.roots[n]); err == nil {
			l = append(l, renamed(fi, n))
		} else {
			l = append(l, dirInfo(n))
		}
	}
	return page(l, &d.pos, count)
}

func page(l []fs.FileInfo, pos *int, count int) ([]fs.FileInfo, error) {
	if count <= 0 {
		out := l[*pos:]
		*pos = len(l)
		return out, nil
	}
	if *pos >= len(l) {
		return nil, io.EOF
	}
	end := min(len(l), *pos+count)
	out := l[*pos:end]
	*pos = end
	return out, nil
}

// renamed gives fi the share's name and a content type (so WebDAV never sniffs).
func renamed(fi os.FileInfo, name string) os.FileInfo { return typed{fi, name} }

type dirInfo string

func (d dirInfo) Name() string       { return string(d) }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (d dirInfo) ModTime() time.Time { return time.Time{} }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() any           { return nil }
