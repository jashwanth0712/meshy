package davfs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/net/webdav"
)

// pipeSFTP connects an sftp client to an in-process server on the real disk.
func pipeSFTP(t *testing.T) *sftp.Client {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	srv, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	c, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cw.Close(); sw.Close(); srv.Close(); c.Close() })
	return c
}

func do(t *testing.T, h http.Handler, method, p, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, p, strings.NewReader(body))
	if method == "PROPFIND" {
		req.Header.Set("Depth", "1")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestShare(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	oct := filepath.Join(base, "oct26")
	secret := filepath.Join(base, "secret")
	os.MkdirAll(filepath.Join(oct, "notes"), 0o755)
	os.WriteFile(filepath.Join(oct, "notes", "a.txt"), []byte("hello"), 0o644)
	os.MkdirAll(secret, 0o755)
	os.Chmod(secret, 0) // like a folder the device shares but this grant can't open
	defer os.Chmod(secret, 0o755)

	fsys, err := New(pipeSFTP(t), []string{oct, secret, "/", "relative"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fsys.Roots()) != 1 || fsys.Roots()["oct26"] != oct {
		t.Fatalf("roots = %v, want only oct26", fsys.Roots())
	}
	h := &webdav.Handler{Prefix: "/jash", FileSystem: fsys, LockSystem: webdav.NewMemLS()}

	if w := do(t, h, "PROPFIND", "/jash/", ""); w.Code != 207 || !strings.Contains(w.Body.String(), "/jash/oct26/") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("root listing %d: %s", w.Code, w.Body)
	}
	if w := do(t, h, "GET", "/jash/oct26/notes/a.txt", ""); w.Code != 200 || w.Body.String() != "hello" {
		t.Fatalf("GET %d %q", w.Code, w.Body)
	}
	if w := do(t, h, "PUT", "/jash/oct26/notes/b.txt", "new"); w.Code/100 != 2 {
		t.Fatalf("PUT %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(oct, "notes", "b.txt")); string(b) != "new" {
		t.Fatalf("PUT did not reach the device: %q", b)
	}
	// Finder's metadata stays on this Mac.
	if w := do(t, h, "PUT", "/jash/oct26/notes/._b.txt", "meta"); w.Code/100 != 2 {
		t.Fatalf("PUT ._ %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(oct, "notes", "._b.txt")); !os.IsNotExist(err) {
		t.Fatal("._ file was written to the device")
	}
	if w := do(t, h, "GET", "/jash/oct26/notes/._b.txt", ""); w.Body.String() != "meta" {
		t.Fatalf("._ file not kept in memory: %q", w.Body)
	}
	if w := do(t, h, "MKCOL", "/jash/oct26/new-dir", ""); w.Code != 201 {
		t.Fatalf("MKCOL %d", w.Code)
	}
	req := httptest.NewRequest("MOVE", "/jash/oct26/notes/b.txt", nil)
	req.Header.Set("Destination", "/jash/oct26/new-dir/c.txt")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code/100 != 2 {
		t.Fatalf("MOVE %d", rw.Code)
	}
	if _, err := os.Stat(filepath.Join(oct, "new-dir", "c.txt")); err != nil {
		t.Fatal("MOVE did not reach the device")
	}
	if w := do(t, h, "DELETE", "/jash/oct26/new-dir", ""); w.Code/100 != 2 {
		t.Fatalf("DELETE %d", w.Code)
	}
	// The share's top level is fixed: the device's roots.
	if w := do(t, h, "MKCOL", "/jash/extra", ""); w.Code/100 == 2 {
		t.Fatal("made a folder at the share root")
	}
	if w := do(t, h, "DELETE", "/jash/oct26", ""); w.Code/100 == 2 {
		t.Fatal("deleted a shared root")
	}
}

func TestNoAccess(t *testing.T) {
	if _, err := New(pipeSFTP(t), []string{"/definitely/not/here"}); err == nil || !strings.Contains(err.Error(), "MESHY_NO_ACCESS") {
		t.Fatalf("err = %v", err)
	}
}
