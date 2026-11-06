//go:build darwin

package sandbox

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/davfs"
	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/meshyd"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// TestMountThroughSeatbelt: the Finder share (davfs over SFTP) runs inside the
// same sandboxed session as `meshy ssh`, so the grant's folders and modes hold.
func TestMountThroughSeatbelt(t *testing.T) {
	sb := &Seatbelt{RunDir: t.TempDir()}
	if err := sb.Check(); err != nil {
		t.Skip(err)
	}
	root := fixture(t)
	rw := filepath.Join(root, "reports")
	ro := filepath.Join(root, "readonly")
	os.MkdirAll(ro, 0o755)
	os.WriteFile(filepath.Join(ro, "r.txt"), []byte("look"), 0o644)
	pol := &policy.Policy{V: 1, Grant: "roshan.jash.sample1.acme.eth", Paths: []policy.PathRule{{Path: rw, Mode: policy.ReadWrite}, {Path: ro, Mode: policy.ReadOnly}}}
	if err := pol.Validate(); err != nil {
		t.Fatal(err)
	}
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	host, _ := gossh.NewSignerFromKey(hk)
	_, ck, _ := ed25519.GenerateKey(rand.Reader)
	client, _ := gossh.NewSignerFromKey(ck)
	ring := &audit.Ring{}
	srv := &meshyd.Server{
		Resource: "jash.sample1.acme.eth", HostKey: host, Sandbox: sb, Audit: ring,
		Auth: meshyd.StaticAuthorizer{"roshan": {Agent: "roshan", Grant: enskit.Grant{Name: "roshan.jash.sample1.acme.eth", TokenID: big.NewInt(1)}, Expiry: time.Now().Add(time.Hour), Policy: pol}},
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	defer srv.Close()

	c, err := gossh.Dial("tcp", ln.Addr().String(), &gossh.ClientConfig{User: "roshan", Auth: []gossh.AuthMethod{gossh.PublicKeys(client)}, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, _ := c.NewSession()
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.Start(davfs.SFTPCommand); err != nil {
		t.Fatal(err)
	}
	sc, err := sftp.NewClientPipe(out, in)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	// secrets is a device root too, but not in the grant: it must not show up.
	share, err := davfs.New(sc, []string{rw, ro, filepath.Join(root, "secrets")})
	if err != nil {
		t.Fatal(err)
	}
	if r := share.Roots(); len(r) != 2 || r["reports"] != rw || r["readonly"] != ro {
		t.Fatalf("roots = %v", r)
	}
	if b, err := readAll(sc, filepath.Join(rw, "q3.csv")); err != nil || b != "revenue,42\n" {
		t.Fatalf("read granted file: %q %v", b, err)
	}
	if f, err := sc.Create(filepath.Join(rw, "new.txt")); err != nil {
		t.Fatalf("write in rw folder: %v", err)
	} else {
		f.Write([]byte("hi"))
		f.Close()
	}
	if f, err := sc.Create(filepath.Join(ro, "nope.txt")); err == nil {
		f.Close()
		t.Fatal("wrote into a read-only folder")
	}
	if _, err := readAll(sc, filepath.Join(root, "secrets", "key")); err == nil {
		t.Fatal("read outside the grant")
	}
	if _, err := os.Stat(filepath.Join(rw, "new.txt")); err != nil {
		t.Fatal("rw write did not land")
	}
}

func readAll(c *sftp.Client, p string) (string, error) {
	f, err := c.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, 1024)
	n, err := f.Read(b)
	if n > 0 {
		err = nil
	}
	return string(b[:n]), err
}
