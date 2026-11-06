package arkivkit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

type fakeExec struct {
	mu    sync.Mutex
	calls [][]Create
	fail  bool
}

func (f *fakeExec) Execute(_ context.Context, c ...Create) (common.Hash, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return common.Hash{}, errors.New("rpc down")
	}
	f.calls = append(f.calls, c)
	return common.Hash{1}, nil
}

func attr(c Create, name string) (Attr, bool) {
	for _, a := range c.Attrs {
		if a.Name == name {
			return a, true
		}
	}
	return Attr{}, false
}

func TestBatcherBuildsEntities(t *testing.T) {
	org, _ := policy.GenerateKey()
	ex := &fakeExec{}
	b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.prod.acme.eth", OrgKey: org.PublicKey()}
	now := time.Now()
	for _, e := range []audit.Event{
		{Kind: audit.SessionStart, TS: now, Agent: "claude-1", Session: "s1", Grant: "claude-1.db1.prod.acme.eth"},
		{Kind: audit.Op, TS: now, Agent: "claude-1", Session: "s1", Op: "read", Path: "/srv/reports/q3.csv", Bytes: 11},
		{Kind: audit.Op, TS: now.Add(time.Second), Agent: "claude-1", Session: "s1", Op: "readdir", Path: "/srv/reports"},
		{Kind: audit.Blocked, TS: now, Agent: "claude-1", Session: "s1", Op: "open", Path: "/etc/shadow", Reason: "outside grant"},
		{Kind: audit.SessionRevoke, TS: now, Agent: "claude-1", Session: "s1", Block: 9123456, Latency: 11800, Reason: "revoked"},
	} {
		b.Emit(e)
	}
	b.Flush(context.Background())
	if len(ex.calls) != 1 {
		t.Fatalf("txs = %d", len(ex.calls))
	}
	creates := ex.calls[0]
	if len(creates) != 4 { // start, blocked, revoke, 1 chunk
		t.Fatalf("entities = %d", len(creates))
	}
	var schemas []string
	for _, c := range creates {
		a, _ := attr(c, "arkiv.schema")
		schemas = append(schemas, string(a.Value))
		if _, err := c.Encode(); err != nil {
			t.Fatalf("encode: %v", err)
		}
		// No plaintext path may leak into any attribute when sealing.
		for _, a := range c.Attrs {
			if strings.Contains(string(a.Value), "/etc/shadow") || strings.Contains(string(a.Value), "q3.csv") {
				t.Fatalf("path leaked in attribute %s", a.Name)
			}
		}
	}
	if got := strings.Join(schemas, ","); got != "meshy.session,meshy.blocked,meshy.session,meshy.audit-chunk" {
		t.Fatalf("schemas = %s", got)
	}
	// The blocked payload decrypts with the org key and has the path.
	p, _ := attr(creates[1], "payload")
	plain, err := policy.OpenBytes(string(p.Value), SchemaBlocked, org)
	if err != nil || !strings.Contains(string(plain), "/etc/shadow") {
		t.Fatalf("open: %v %s", err, plain)
	}
	n, _ := attr(creates[3], "nOps")
	if n.Value[31] != 2 {
		t.Fatalf("nOps = %x", n.Value)
	}
}

// TestSessionLifetimes verifies per-type entity lifetimes match compliance windows.
func TestSessionLifetimes(t *testing.T) {
	now := time.Now()

	t.Run("blocked_30days", func(t *testing.T) {
		ex := &fakeExec{}
		b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.acme.eth"}
		b.Emit(audit.Event{Kind: audit.Blocked, TS: now, Op: "open", Reason: "outside grant"})
		b.Flush(context.Background())
		if len(ex.calls) == 0 {
			t.Fatal("no calls")
		}
		c := ex.calls[0][0]
		if c.MinLifetime != Lifetime30Days {
			t.Fatalf("blocked lifetime = %d, want %d", c.MinLifetime, Lifetime30Days)
		}
	})

	t.Run("chunk_30days", func(t *testing.T) {
		ex := &fakeExec{}
		b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.acme.eth"}
		b.Emit(audit.Event{Kind: audit.Op, TS: now, Session: "s1", Op: "read"})
		b.Flush(context.Background())
		if len(ex.calls) == 0 {
			t.Fatal("no calls")
		}
		c := ex.calls[0][0]
		if c.MinLifetime != Lifetime30Days {
			t.Fatalf("chunk lifetime = %d, want %d", c.MinLifetime, Lifetime30Days)
		}
	})

	t.Run("revoke_90days", func(t *testing.T) {
		ex := &fakeExec{}
		b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.acme.eth"}
		b.Emit(audit.Event{Kind: audit.SessionRevoke, TS: now, Session: "s1", Block: 100, Latency: 150})
		b.Flush(context.Background())
		if len(ex.calls) == 0 {
			t.Fatal("no calls")
		}
		c := ex.calls[0][0]
		if c.MinLifetime != Lifetime90Days {
			t.Fatalf("revoke lifetime = %d, want %d", c.MinLifetime, Lifetime90Days)
		}
	})

	t.Run("session_start_grant_expiry", func(t *testing.T) {
		ex := &fakeExec{}
		b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.acme.eth"}
		grantExpiry := now.Add(2 * time.Hour) // grant expires in 2 hours
		b.Emit(audit.Event{Kind: audit.SessionStart, TS: now, Session: "s1", GrantExpiry: grantExpiry})
		b.Flush(context.Background())
		if len(ex.calls) == 0 {
			t.Fatal("no calls")
		}
		c := ex.calls[0][0]
		// Expected: (2h + 1d) / 2s = (7200 + 86400) / 2 = 46800 blocks
		wantMin := uint64((2*3600 + 86400) / 2)
		wantMax := uint64((2*3600 + 86400) / 2)
		// Allow ±5 blocks for timing jitter in tests
		if c.MinLifetime < wantMin-5 || c.MinLifetime > wantMax+5 {
			t.Fatalf("session start lifetime = %d, want ~%d", c.MinLifetime, wantMin)
		}
	})

	t.Run("session_start_no_expiry_fallback", func(t *testing.T) {
		ex := &fakeExec{}
		b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.acme.eth"}
		b.Emit(audit.Event{Kind: audit.SessionStart, TS: now, Session: "s1"}) // no GrantExpiry
		b.Flush(context.Background())
		if len(ex.calls) == 0 {
			t.Fatal("no calls")
		}
		c := ex.calls[0][0]
		if c.MinLifetime != Lifetime30Days {
			t.Fatalf("fallback lifetime = %d, want %d", c.MinLifetime, Lifetime30Days)
		}
	})
}

func TestBatcherRetriesOnFailure(t *testing.T) {
	ex := &fakeExec{fail: true}
	b := &Batcher{Exec: ex, Org: "acme.eth", Resource: "db1.prod.acme.eth"}
	b.Emit(audit.Event{Kind: audit.Blocked, TS: time.Now(), Path: "/etc/shadow"})
	b.Flush(context.Background())
	ex.fail = false
	b.Flush(context.Background())
	if len(ex.calls) != 1 || len(ex.calls[0]) != 1 {
		t.Fatalf("calls = %+v", ex.calls)
	}
	if a, ok := attr(ex.calls[0][0], "path"); !ok || string(a.Value) != "/etc/shadow" {
		t.Fatal("dev mode (no org key) should include plain path attr")
	}
}
