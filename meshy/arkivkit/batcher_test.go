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
