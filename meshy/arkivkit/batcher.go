package arkivkit

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdh"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/jashwanth0712/meshy/meshy/audit"
	"github.com/jashwanth0712/meshy/meshy/policy"
)

// Entity schemas (see /arkiv/schema.md).
const (
	SchemaSession = "meshy.session" // session start / end / revoke (with latency)
	SchemaBlocked = "meshy.blocked" // one per blocked attempt or denied login
	SchemaChunk   = "meshy.audit-chunk"
)

// Executor sends a batch (Writer implements it).
type Executor interface {
	Execute(ctx context.Context, creates ...Create) (common.Hash, error)
}

// Batcher is an audit.Sink that buffers events and writes them to Arkiv.
//
//   - session.* and blocked/denied events become individual, indexed
//     entities so the dashboard can query and watch them cheaply;
//   - allowed ops are rolled into one meshy.audit-chunk per session per
//     flush, payload = gzip(JSONL), so volume stays low;
//   - detailed fields (paths, argv) live only in the payload, sealed to the
//     org key (Arkiv is public). Indexed attributes carry metadata only.
type Batcher struct {
	Exec     Executor
	Org      string
	Resource string
	OrgKey   *ecdh.PublicKey // nil = payloads in clear (dev only)
	Interval time.Duration   // default 5s
	MaxBatch int             // events per flush trigger; default 100
	MaxOps   int             // creates per tx; default 25
	Lifetime uint64          // blocks; default 30 days
	Log      *slog.Logger
	// OnFlush is called after each successful transaction.
	OnFlush func(tx common.Hash, entities int)

	mu    sync.Mutex
	buf   []audit.Event
	kick  chan struct{}
	start sync.Once
}

func (b *Batcher) log() *slog.Logger {
	if b.Log == nil {
		return slog.Default()
	}
	return b.Log
}

func (b *Batcher) Emit(e audit.Event) {
	b.start.Do(func() { b.kick = make(chan struct{}, 1) })
	b.mu.Lock()
	b.buf = append(b.buf, e)
	if len(b.buf) > 10_000 { // backpressure: drop oldest allowed ops first
		b.buf = dropOps(b.buf, 2_000)
	}
	full := len(b.buf) >= b.maxBatch()
	urgent := e.Kind == audit.SessionRevoke || e.Kind == audit.Blocked
	b.mu.Unlock()
	if full || urgent {
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
}

func (b *Batcher) maxBatch() int {
	if b.MaxBatch == 0 {
		return 100
	}
	return b.MaxBatch
}

// Run flushes periodically until ctx ends, then does a final flush.
func (b *Batcher) Run(ctx context.Context) {
	b.start.Do(func() { b.kick = make(chan struct{}, 1) })
	iv := b.Interval
	if iv == 0 {
		iv = 5 * time.Second
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			b.Flush(fctx)
			cancel()
			return
		case <-t.C:
		case <-b.kick:
			// Coalesce a burst (e.g. several blocked attempts) into one tx.
			time.Sleep(750 * time.Millisecond)
		}
		b.Flush(ctx)
	}
}

// Flush writes everything buffered. On failure events are put back.
func (b *Batcher) Flush(ctx context.Context) {
	b.mu.Lock()
	evs := b.buf
	b.buf = nil
	b.mu.Unlock()
	if len(evs) == 0 {
		return
	}
	creates := b.build(evs)
	maxOps := b.MaxOps
	if maxOps == 0 {
		maxOps = 25
	}
	for i := 0; i < len(creates); i += maxOps {
		j := min(i+maxOps, len(creates))
		tx, err := b.Exec.Execute(ctx, creates[i:j]...)
		if err != nil {
			b.log().Error("arkiv flush failed; will retry", "err", err, "entities", j-i)
			b.mu.Lock()
			b.buf = append(evs, b.buf...) // simplest correct retry: rebuild everything next time
			b.mu.Unlock()
			return
		}
		b.log().Info("arkiv batch written", "tx", tx.Hex(), "entities", j-i)
		if b.OnFlush != nil {
			b.OnFlush(tx, j-i)
		}
	}
}

func (b *Batcher) life() uint64 {
	if b.Lifetime == 0 {
		return Lifetime30Days
	}
	return b.Lifetime
}

// sessionLife returns the MinLifetime (blocks) for a session entity:
//   - On SessionStart/SessionEnd: grant expiry + 1 day, converted to Rabdi
//     blocks (2 s each), so the entity lives exactly as long as the grant's
//     evidence window.  Falls back to 30 days when expiry is not set or is
//     already past.
//   - On SessionRevoke: 90 days (3,888,000 blocks) for dispute / slashing
//     evidence.
func sessionLife(e audit.Event) uint64 {
	if e.Kind == audit.SessionRevoke {
		return Lifetime90Days
	}
	if !e.GrantExpiry.IsZero() && e.GrantExpiry.After(time.Now()) {
		margin := time.Duration(24 * time.Hour) // 1-day buffer after grant ends
		remaining := time.Until(e.GrantExpiry) + margin
		blocks := uint64(remaining.Seconds()) / RabdiBlockSecs
		if blocks < 1 {
			blocks = 1
		}
		if blocks > MaxLifetime {
			blocks = MaxLifetime
		}
		return blocks
	}
	return Lifetime30Days
}

func (b *Batcher) seal(plain []byte, aad string) []byte {
	if b.OrgKey == nil {
		return plain
	}
	s, err := policy.SealBytes(plain, aad, b.OrgKey)
	if err != nil {
		b.log().Error("seal failed", "err", err)
		return nil
	}
	return []byte(s)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (b *Batcher) common(schema string, e audit.Event) []Attr {
	attrs := []Attr{
		Schema(schema),
		Str("org", trunc(b.Org, 128), true),
		Str("resource", trunc(b.Resource, 128), true),
		U64("ts", uint64(e.TS.UnixMilli()), true),
	}
	if e.Agent != "" {
		attrs = append(attrs, Str("agent", trunc(e.Agent, 128), true))
	}
	if e.Grant != "" {
		attrs = append(attrs, Str("grant", trunc(e.Grant, 128), true))
	}
	if e.Session != "" {
		attrs = append(attrs, Str("session", e.Session, true))
	}
	return attrs
}

func (b *Batcher) build(evs []audit.Event) []Create {
	var out []Create
	chunks := map[string][]audit.Event{}
	for _, e := range evs {
		switch e.Kind {
		case audit.Op:
			chunks[e.Session] = append(chunks[e.Session], e)
		case audit.Blocked, audit.Denied:
			attrs := append(b.common(SchemaBlocked, e),
				Str("kind", string(e.Kind), true),
				Str("op", trunc(e.Op, 32), true),
				Str("reason", trunc(e.Reason, 128), true),
			)
			if b.OrgKey == nil && e.Path != "" {
				attrs = append(attrs, Str("path", trunc(e.Path, 128), false))
			}
			payload, _ := json.Marshal(e)
			attrs = append(attrs, Bytes("payload", b.seal(payload, SchemaBlocked)))
			// meshy.blocked: 30-day compliance window
			out = append(out, Create{MinLifetime: Lifetime30Days, Flags: FlagReadOnly, Attrs: attrs})
		case audit.SessionStart, audit.SessionEnd, audit.SessionRevoke:
			attrs := append(b.common(SchemaSession, e), Str("kind", string(e.Kind), true))
			if e.Kind == audit.SessionRevoke {
				attrs = append(attrs,
					U64("block", e.Block, true),
					U64("latencyMs", uint64(e.Latency), true),
					Str("reason", trunc(e.Reason, 128), true),
				)
			}
			payload, _ := json.Marshal(e)
			attrs = append(attrs, Bytes("payload", b.seal(payload, SchemaSession)))
			// session.start/end: live until grant expiry + 1 day (falls back to 30d)
			// session.revoke: 90 days for dispute / slashing evidence
			out = append(out, Create{MinLifetime: sessionLife(e), Flags: FlagReadOnly, Attrs: attrs})
		}
	}
	sessions := make([]string, 0, len(chunks))
	for s := range chunks {
		sessions = append(sessions, s)
	}
	sort.Strings(sessions)
	for _, s := range sessions {
		ops := chunks[s]
		var raw bytes.Buffer
		gz := gzip.NewWriter(&raw)
		enc := json.NewEncoder(gz)
		for _, e := range ops {
			enc.Encode(e)
		}
		gz.Close()
		first, last := ops[0], ops[len(ops)-1]
		attrs := append(b.common(SchemaChunk, first),
			U64("tsEnd", uint64(last.TS.UnixMilli()), true),
			U64("nOps", uint64(len(ops)), true),
			Bytes("payload", b.seal(raw.Bytes(), SchemaChunk)),
		)
		// meshy.audit-chunk: 30-day compliance window
		out = append(out, Create{MinLifetime: Lifetime30Days, Flags: FlagReadOnly, Attrs: attrs})
	}
	return out
}

func dropOps(evs []audit.Event, n int) []audit.Event {
	out := evs[:0]
	for _, e := range evs {
		if n > 0 && e.Kind == audit.Op {
			n--
			continue
		}
		out = append(out, e)
	}
	return out
}
