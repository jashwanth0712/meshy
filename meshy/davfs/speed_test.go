package davfs

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/net/webdav"
)

// counting wraps the SFTP client and counts the requests that cross the mesh.
type counting struct {
	*sftp.Client
	n atomic.Int64
}

func (c *counting) Stat(p string) (os.FileInfo, error)      { c.n.Add(1); return c.Client.Stat(p) }
func (c *counting) ReadDir(p string) ([]os.FileInfo, error) { c.n.Add(1); return c.Client.ReadDir(p) }
func (c *counting) OpenFile(p string, f int) (*sftp.File, error) {
	c.n.Add(1)
	return c.Client.OpenFile(p, f)
}

// A Finder-style listing of a folder with 40 files must not cost a round trip per file.
func TestListingRoundTrips(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	dir := filepath.Join(base, "oct26")
	os.MkdirAll(dir, 0o755)
	for i := 0; i < 40; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte("hello"), 0o644)
	}
	big := bytes.Repeat([]byte("0123456789abcdef"), 3<<16) // 3 MiB
	os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644)

	c := &counting{Client: pipeSFTP(t)}
	fsys, err := New(c, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	h := &webdav.Handler{Prefix: "/jash", FileSystem: fsys, LockSystem: webdav.NewMemLS()}
	c.n.Store(0)
	w := do(t, h, "PROPFIND", "/jash/oct26/", "")
	if w.Code != 207 || strings.Count(w.Body.String(), "<D:response>") != 42 {
		t.Fatalf("listing %d: %d entries", w.Code, strings.Count(w.Body.String(), "<D:response>"))
	}
	if n := c.n.Load(); n > 3 {
		t.Fatalf("listing 41 files took %d SFTP round trips; want ≤ 3", n)
	}
	// Opening a file to read it: one open, then big read-ahead chunks.
	c.n.Store(0)
	g := do(t, h, "GET", "/jash/oct26/big.bin", "")
	if !bytes.Equal(g.Body.Bytes(), big) {
		t.Fatalf("GET returned %d bytes, want %d", g.Body.Len(), len(big))
	}
	if n := c.n.Load(); n > 2 {
		t.Fatalf("GET took %d open/stat round trips", n)
	}
	r := httptest.NewRequest("GET", "/jash/oct26/big.bin", nil)
	r.Header.Set("Range", "bytes=2000000-2000009")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, r)
	if rw.Code != 206 || !bytes.Equal(rw.Body.Bytes(), big[2000000:2000010]) {
		t.Fatalf("range read %d %q", rw.Code, rw.Body.Bytes())
	}
}
