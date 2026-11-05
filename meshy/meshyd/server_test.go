package meshyd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/enskit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

type fakeWatcher struct {
	mu      sync.Mutex
	watched map[string]enskit.Grant
	healthy bool
}

func (f *fakeWatcher) Watch(id string, g enskit.Grant) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watched[id] = g
}
func (f *fakeWatcher) Unwatch(id string) { f.mu.Lock(); defer f.mu.Unlock(); delete(f.watched, id) }
func (f *fakeWatcher) Healthy(time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthy
}
func (f *fakeWatcher) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id := range f.watched {
		out = append(out, id)
	}
	return out
}

func signer(t *testing.T) gossh.Signer {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	s, err := gossh.NewSignerFromKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type harness struct {
	srv  *Server
	addr string
	w    *fakeWatcher
	ring *audit.Ring
}

func newHarness(t *testing.T, expiry time.Time) *harness {
	dir := t.TempDir()
	pol := &policy.Policy{V: 1, Grant: "claude-1.db1.prod.acme.eth", Paths: []policy.PathRule{{Path: dir, Mode: policy.ReadOnly}}}
	if err := pol.Validate(); err != nil {
		t.Fatal(err)
	}
	w := &fakeWatcher{watched: map[string]enskit.Grant{}, healthy: true}
	ring := &audit.Ring{}
	srv := &Server{
		Resource: "db1.prod.acme.eth",
		HostKey:  signer(t),
		Auth: StaticAuthorizer{"claude-1": {
			Agent: "claude-1", Grant: enskit.Grant{Name: "claude-1.db1.prod.acme.eth", TokenID: big.NewInt(1)},
			Expiry: expiry, Policy: pol,
		}},
		Sandbox: UnsafeSandbox{},
		Watcher: w,
		Audit:   ring,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return &harness{srv: srv, addr: ln.Addr().String(), w: w, ring: ring}
}

func (h *harness) run(t *testing.T, user, cmd string) (stdout, stderr string, err error) {
	t.Helper()
	cfg := &gossh.ClientConfig{
		User: user, Auth: []gossh.AuthMethod{gossh.PublicKeys(signer(t))},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	}
	c, err := gossh.Dial("tcp", h.addr, cfg)
	if err != nil {
		return "", "", err
	}
	defer c.Close()
	s, err := c.NewSession()
	if err != nil {
		return "", "", err
	}
	var o, e bytes.Buffer
	s.Stdout, s.Stderr = &o, &e
	err = s.Run(cmd)
	return o.String(), e.String(), err
}

func exitCode(err error) int {
	var ee *gossh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus()
	}
	if err == nil {
		return 0
	}
	return -1
}

func TestRunsCommandForGrantedAgent(t *testing.T) {
	h := newHarness(t, time.Now().Add(time.Hour))
	out, _, err := h.run(t, "claude-1", "echo hello")
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	kinds := map[audit.Kind]int{}
	for _, e := range h.ring.Snapshot() {
		kinds[e.Kind]++
	}
	if kinds[audit.SessionStart] != 1 || kinds[audit.SessionEnd] != 1 {
		t.Fatalf("events %v", kinds)
	}
}

func TestDeniesUnknownAgent(t *testing.T) {
	h := newHarness(t, time.Now().Add(time.Hour))
	if _, _, err := h.run(t, "mallory", "id"); err == nil {
		t.Fatal("unknown agent was admitted")
	}
	evs := h.ring.Snapshot()
	if len(evs) == 0 || evs[len(evs)-1].Kind != audit.Denied || !strings.Contains(evs[len(evs)-1].Reason, "MESHY_NO_GRANT") {
		t.Fatalf("events %+v", evs)
	}
}

func TestFailsClosedWhenChainStale(t *testing.T) {
	h := newHarness(t, time.Now().Add(time.Hour))
	h.w.healthy = false
	if _, _, err := h.run(t, "claude-1", "id"); err == nil {
		t.Fatal("admitted while chain view stale")
	}
}

func TestRevokeKillsLiveSession(t *testing.T) {
	h := newHarness(t, time.Now().Add(time.Hour))
	type res struct {
		stderr string
		err    error
	}
	done := make(chan res, 1)
	go func() {
		_, e, err := h.run(t, "claude-1", "sleep 30; echo survived")
		done <- res{e, err}
	}()
	var id string
	for deadline := time.Now().Add(3 * time.Second); id == "" && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ids := h.w.ids(); len(ids) == 1 {
			id = ids[0]
		}
	}
	if id == "" {
		t.Fatal("session never registered with watcher")
	}
	start := time.Now()
	h.srv.Revoke(enskit.Revocation{SessionID: id, Grant: "claude-1.db1.prod.acme.eth", Reason: "revoked or expired", Block: big.NewInt(9123456), HeadSeen: start})
	select {
	case r := <-done:
		if el := time.Since(start); el > time.Second {
			t.Fatalf("kill took %s", el)
		}
		if exitCode(r.err) != 137 {
			t.Fatalf("exit = %v", r.err)
		}
		if !strings.Contains(r.stderr, "MESHY_REVOKED grant=claude-1.db1.prod.acme.eth block=9123456") {
			t.Fatalf("stderr = %q", r.stderr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session survived revocation")
	}
	var revs int
	for _, e := range h.ring.Snapshot() {
		if e.Kind == audit.SessionRevoke {
			revs++
		}
	}
	if revs != 1 {
		t.Fatalf("revoke events = %d", revs)
	}
}

func TestExpiryTimerKillsSession(t *testing.T) {
	h := newHarness(t, time.Now().Add(1500*time.Millisecond))
	start := time.Now()
	_, stderr, err := h.run(t, "claude-1", "sleep 30")
	if time.Since(start) > 4*time.Second || exitCode(err) != 137 || !strings.Contains(stderr, "MESHY_EXPIRED") {
		t.Fatalf("err=%v stderr=%q after %s", err, stderr, time.Since(start))
	}
}

func TestCommandAllowList(t *testing.T) {
	h := newHarness(t, time.Now().Add(time.Hour))
	az := h.srv.Auth.(StaticAuthorizer)["claude-1"]
	az.Policy.Cmds = []string{"cat"}
	_, stderr, err := h.run(t, "claude-1", "rm -rf x")
	if exitCode(err) != 126 || !strings.Contains(stderr, "MESHY_BLOCKED cmd=rm") {
		t.Fatalf("err=%v stderr=%q", err, stderr)
	}
}
