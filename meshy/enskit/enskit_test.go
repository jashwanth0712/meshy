package enskit

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestNamehash(t *testing.T) {
	// ENSIP-1 test vectors.
	if got := Namehash("").Hex(); got != "0x0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatal(got)
	}
	if got := Namehash("eth").Hex(); got != "0x93cdeb708b7545dc668eb9280176169d1c33cfd8ed6f04690a0bcc88a93fc4ae" {
		t.Fatal(got)
	}
	if got := Namehash("foo.eth").Hex(); got != "0xde9b09fd7c5f901e23a3f19fecc54828e9c848539801e86591bd9801b019f84f" {
		t.Fatal(got)
	}
}

func TestDNSEncode(t *testing.T) {
	b, err := DNSEncode("db1.prod.acme.eth")
	if err != nil {
		t.Fatal(err)
	}
	want := "\x03db1\x04prod\x04acme\x03eth\x00"
	if string(b) != want {
		t.Fatalf("%q", b)
	}
	if _, err := DNSEncode("a..eth"); err == nil {
		t.Fatal("empty label accepted")
	}
}

func TestNames(t *testing.T) {
	res := Name("db1.prod.acme.eth")
	g, err := GrantName("claude-1", res)
	if err != nil || g != "claude-1.db1.prod.acme.eth" {
		t.Fatal(g, err)
	}
	a, _ := AgentName("claude-1", res)
	if a != "claude-1.agents.acme.eth" {
		t.Fatal(a)
	}
	if g.Label() != "claude-1" || g.Parent() != res || g.Org() != "acme.eth" {
		t.Fatal("name helpers")
	}
	for _, bad := range []string{"", "Claude", "a.b", "x/y"} {
		if _, err := GrantName(bad, res); !errors.Is(err, ErrBadName) {
			t.Errorf("label %q accepted", bad)
		}
	}
	if _, err := GrantName("claude-1", "acme.eth"); err == nil {
		t.Fatal("non-resource accepted")
	}
}

// --- watcher tests with fakes ---

type fakeHeads struct{ ch chan Head }

func (f fakeHeads) Heads(context.Context) (<-chan Head, error) { return f.ch, nil }

type fakeChecker struct {
	mu      sync.Mutex
	verdict map[Name]Verdict
	err     map[Name]error
}

func (f *fakeChecker) set(n Name, v Verdict) { f.mu.Lock(); f.verdict[n] = v; f.mu.Unlock() }
func (f *fakeChecker) fail(n Name, e error)  { f.mu.Lock(); f.err[n] = e; f.mu.Unlock() }
func (f *fakeChecker) CheckGrant(_ context.Context, g Grant, _ *big.Int) (Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.err[g.Name]; e != nil {
		return Verdict{}, e
	}
	if v, ok := f.verdict[g.Name]; ok {
		return v, nil
	}
	return Verdict{Live: true}, nil
}

func setup(t *testing.T, maxAge time.Duration) (*Watcher, fakeHeads, *fakeChecker, chan Revocation) {
	heads := fakeHeads{make(chan Head)}
	chk := &fakeChecker{verdict: map[Name]Verdict{}, err: map[Name]error{}}
	revs := make(chan Revocation, 16)
	w := &Watcher{Heads: heads, Checker: chk, MaxHeadAge: maxAge, OnRevoke: func(r Revocation) { revs <- r }}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)
	return w, heads, chk, revs
}

func head(n int64) Head { return Head{Number: big.NewInt(n), SeenAt: time.Now()} }

func expectRevoke(t *testing.T, revs chan Revocation, session string) Revocation {
	t.Helper()
	select {
	case r := <-revs:
		if r.SessionID != session {
			t.Fatalf("revoked %s, want %s", r.SessionID, session)
		}
		return r
	case <-time.After(3 * time.Second):
		t.Fatalf("no revocation for %s", session)
	}
	return Revocation{}
}

func expectNone(t *testing.T, revs chan Revocation) {
	t.Helper()
	select {
	case r := <-revs:
		t.Fatalf("unexpected revocation %+v", r)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWatcherRevokesDeadGrant(t *testing.T) {
	w, heads, chk, revs := setup(t, time.Minute)
	a, b := Name("claude-1.db1.prod.acme.eth"), Name("codex.db1.prod.acme.eth")
	w.Watch("s1", Grant{Name: a, Owner: common.HexToAddress("0x1")})
	w.Watch("s2", Grant{Name: b, Owner: common.HexToAddress("0x2")})

	heads.ch <- head(1)
	expectNone(t, revs)

	chk.set(a, Verdict{Reason: "revoked or expired"})
	heads.ch <- head(2)
	r := expectRevoke(t, revs, "s1")
	if r.Block.Int64() != 2 || r.Grant != a {
		t.Fatalf("%+v", r)
	}
	expectNone(t, revs) // s2 still live, s1 not revoked twice
	heads.ch <- head(3)
	expectNone(t, revs)
}

func TestWatcherFailsClosedOnStaleHead(t *testing.T) {
	w, heads, _, revs := setup(t, 1500*time.Millisecond)
	w.Watch("s1", Grant{Name: "claude-1.db1.prod.acme.eth"})
	heads.ch <- head(1)
	if !w.Healthy(time.Now()) {
		t.Fatal("should be healthy right after a head")
	}
	r := expectRevoke(t, revs, "s1")
	if r.Block != nil {
		t.Fatal("fail-closed revocation should have no block")
	}
	if w.Healthy(time.Now()) {
		t.Fatal("should be unhealthy after stale head")
	}
}

func TestWatcherFailsClosedOnRepeatedErrors(t *testing.T) {
	w, heads, chk, revs := setup(t, time.Minute)
	g := Name("claude-1.db1.prod.acme.eth")
	w.Watch("s1", Grant{Name: g})
	chk.fail(g, errors.New("rpc down"))
	heads.ch <- head(1)
	heads.ch <- head(2)
	expectNone(t, revs)
	heads.ch <- head(3)
	expectRevoke(t, revs, "s1")
}

func TestWatcherUnwatch(t *testing.T) {
	w, heads, chk, revs := setup(t, time.Minute)
	g := Name("claude-1.db1.prod.acme.eth")
	w.Watch("s1", Grant{Name: g})
	w.Unwatch("s1")
	chk.set(g, Verdict{Reason: "expired"})
	heads.ch <- head(1)
	expectNone(t, revs)
}

func TestClampTip(t *testing.T) {
	g := func(n int64) *big.Int { return big.NewInt(n) }
	for _, c := range []struct{ in, want *big.Int }{
		{nil, g(1e9)}, {g(25), g(1e9)}, {g(3e9), g(3e9)}, {g(5e12), g(1e10)},
	} {
		if got := ClampTip(c.in); got.Cmp(c.want) != 0 {
			t.Errorf("ClampTip(%v) = %v", c.in, got)
		}
	}
}
