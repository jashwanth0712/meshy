package davfs

// Speed over the mesh. Every SFTP request is a round trip through the mesh
// (~100 ms), and WebDAV is chatty: for each file in a listing it opens the
// file, stats it and opens it again to sniff the content type. So:
//   - listings fill a short-lived stat cache; Stat answers from it
//   - files opened for reading are lazy: nothing goes over SFTP until a read
//   - file infos carry a content type (by extension): no sniffing
//   - reads go through a 1 MiB read-ahead (pkg/sftp issues it as parallel requests)

import (
	"context"
	"io"
	"mime"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

const (
	statTTL   = 10 * time.Second
	readAhead = 1 << 20
)

type cached struct {
	fi os.FileInfo
	at time.Time
}

type statCache struct {
	mu sync.Mutex
	m  map[string]cached
}

func (c *statCache) get(p string) (os.FileInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[p]
	if !ok || time.Since(e.at) > statTTL {
		return nil, false
	}
	return e.fi, true
}

func (c *statCache) put(p string, fi os.FileInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cached{}
	}
	c.m[p] = cached{fi, time.Now()}
}

// drop forgets p, everything under it, and its parent's listing.
func (c *statCache) drop(p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if k == p || strings.HasPrefix(k, p+"/") || k == path.Dir(p) {
			delete(c.m, k)
		}
	}
}

// remoteStat stats a device path, from the cache when it can.
func (f *FS) remoteStat(p string) (os.FileInfo, error) {
	if fi, ok := f.cache.get(p); ok {
		return fi, nil
	}
	fi, err := f.r.Stat(p)
	if err != nil {
		return nil, err
	}
	f.cache.put(p, fi)
	return fi, nil
}

// remoteList lists a device folder and caches every entry's stat.
func (f *FS) remoteList(p string) ([]os.FileInfo, error) {
	l, err := f.r.ReadDir(p)
	if err != nil {
		return nil, err
	}
	for _, fi := range l {
		f.cache.put(path.Join(p, fi.Name()), fi)
	}
	return l, nil
}

// typed is a FileInfo that names its content type, so WebDAV never opens the
// file to sniff it (webdav.ContentTyper).
type typed struct {
	os.FileInfo
	name string
}

func (t typed) Name() string { return t.name }
func (t typed) ContentType(context.Context) (string, error) {
	if t.IsDir() {
		return "httpd/unix-directory", nil
	}
	if ct := mime.TypeByExtension(path.Ext(t.name)); ct != "" {
		return ct, nil
	}
	return "application/octet-stream", nil
}

// lazyFile opens the remote file on first read and reads ahead in big chunks.
type lazyFile struct {
	f    *FS
	p    string
	name string
	fi   os.FileInfo

	mu    sync.Mutex
	h     *sftp.File
	pos   int64
	buf   []byte
	bufAt int64
}

func (l *lazyFile) open() error {
	if l.h != nil {
		return nil
	}
	h, err := l.f.r.OpenFile(l.p, os.O_RDONLY)
	if err != nil {
		return err
	}
	l.h = h
	return nil
}

func (l *lazyFile) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pos >= l.fi.Size() {
		return 0, io.EOF
	}
	if l.buf == nil || l.pos < l.bufAt || l.pos >= l.bufAt+int64(len(l.buf)) {
		if err := l.open(); err != nil {
			return 0, err
		}
		b := make([]byte, min(int64(readAhead), l.fi.Size()-l.pos))
		n, err := l.h.ReadAt(b, l.pos)
		if n == 0 && err != nil {
			return 0, err
		}
		l.buf, l.bufAt = b[:n], l.pos
	}
	n := copy(p, l.buf[l.pos-l.bufAt:])
	l.pos += int64(n)
	return n, nil
}

func (l *lazyFile) Seek(off int64, whence int) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch whence {
	case io.SeekStart:
		l.pos = off
	case io.SeekCurrent:
		l.pos += off
	case io.SeekEnd:
		l.pos = l.fi.Size() + off
	}
	if l.pos < 0 {
		l.pos = 0
		return 0, os.ErrInvalid
	}
	return l.pos, nil
}

func (l *lazyFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.h != nil {
		return l.h.Close()
	}
	return nil
}

func (l *lazyFile) Stat() (os.FileInfo, error)         { return typed{l.fi, l.name}, nil }
func (l *lazyFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (l *lazyFile) Write([]byte) (int, error)          { return 0, os.ErrPermission }
