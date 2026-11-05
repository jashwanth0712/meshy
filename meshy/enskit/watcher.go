package enskit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Head is a new chain head.
type Head struct {
	Number *big.Int
	Time   uint64 // block timestamp, seconds
	SeenAt time.Time
}

// HeadSource streams chain heads. Implementations: WSHeads (eth_subscribe)
// and PollHeads (HTTP fallback). The channel closes when ctx ends.
type HeadSource interface {
	Heads(ctx context.Context) (<-chan Head, error)
}

// GrantChecker evaluates one grant at one block. ENSClient implements it.
type GrantChecker interface {
	CheckGrant(ctx context.Context, g Grant, block *big.Int) (Verdict, error)
}

// Grant is a live session's claim on ENS, captured at session start.
type Grant struct {
	Name    Name
	Owner   common.Address // agent address the grant must stay owned by
	TokenID *big.Int       // token id at issue; a change means re-registration
}

// Verdict is the result of checking a grant.
type Verdict struct {
	Live   bool
	Reason string // set when !Live: "unregistered", "expired", "owner changed", ...
}

// CheckGrant implements GrantChecker against ENSv2.
func (c *Client) CheckGrant(ctx context.Context, g Grant, block *big.Int) (Verdict, error) {
	owner, err := c.ExactOwner(ctx, g.Name, block)
	if err != nil {
		return Verdict{}, err
	}
	if owner == (common.Address{}) {
		return Verdict{Reason: "revoked or expired (grant or an ancestor is no longer registered)"}, nil
	}
	if owner != g.Owner {
		return Verdict{Reason: fmt.Sprintf("owner changed to %s", owner.Hex())}, nil
	}
	reg, err := c.ParentRegistry(ctx, g.Name, block)
	if err != nil {
		return Verdict{}, err
	}
	st, err := c.State(ctx, reg, g.Name.Label(), block)
	if err != nil {
		return Verdict{}, err
	}
	if st.Status != Registered {
		return Verdict{Reason: "grant status " + st.Status.String()}, nil
	}
	if g.TokenID != nil && st.TokenID.Cmp(g.TokenID) != 0 {
		return Verdict{Reason: "grant was re-issued (token id changed)"}, nil
	}
	return Verdict{Live: true}, nil
}

// Revocation is delivered when a watched grant stops being live.
type Revocation struct {
	SessionID string
	Grant     Name
	Reason    string
	Block     *big.Int // nil for fail-closed revocations
	HeadSeen  time.Time
}

// Watcher re-checks every watched grant on each new head and revokes
// sessions whose grants died. It fails closed: if no head arrives within
// MaxHeadAge, or a check errors repeatedly, all sessions are revoked.
type Watcher struct {
	Heads      HeadSource
	Checker    GrantChecker
	MaxHeadAge time.Duration // default 45s
	MaxErrors  int           // consecutive check errors before failing closed; default 3
	OnRevoke   func(Revocation)
	Log        *slog.Logger

	mu       sync.Mutex
	sessions map[string]*watched
	lastHead Head
}

type watched struct {
	grant  Grant
	errors int
}

// Watch registers a session. The caller should already have verified the
// grant is live at session start.
func (w *Watcher) Watch(sessionID string, g Grant) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessions == nil {
		w.sessions = map[string]*watched{}
	}
	w.sessions[sessionID] = &watched{grant: g}
}

// Unwatch removes a session (it ended normally).
func (w *Watcher) Unwatch(sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.sessions, sessionID)
}

// LastHead returns the most recent head seen.
func (w *Watcher) LastHead() Head {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastHead
}

// Healthy reports whether the chain view is fresh enough to admit sessions.
func (w *Watcher) Healthy(now time.Time) bool {
	h := w.LastHead()
	return !h.SeenAt.IsZero() && now.Sub(h.SeenAt) <= w.maxHeadAge()
}

func (w *Watcher) maxHeadAge() time.Duration {
	if w.MaxHeadAge == 0 {
		return 45 * time.Second
	}
	return w.MaxHeadAge
}

func (w *Watcher) maxErrors() int {
	if w.MaxErrors == 0 {
		return 3
	}
	return w.MaxErrors
}

func (w *Watcher) log() *slog.Logger {
	if w.Log == nil {
		return slog.Default()
	}
	return w.Log
}

// Run processes heads until ctx is done. It returns only on ctx end or if
// the head source cannot be opened.
func (w *Watcher) Run(ctx context.Context) error {
	heads, err := w.Heads.Heads(ctx)
	if err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case h, ok := <-heads:
			if !ok {
				// Source died: fail closed, then wait for ctx (supervisor restarts us).
				w.failClosed("head source closed")
				<-ctx.Done()
				return errors.Join(ctx.Err(), errors.New("enskit: head source closed"))
			}
			w.onHead(ctx, h)
		case now := <-tick.C:
			w.mu.Lock()
			stale := !w.lastHead.SeenAt.IsZero() && now.Sub(w.lastHead.SeenAt) > w.maxHeadAge()
			w.mu.Unlock()
			if stale {
				w.failClosed(fmt.Sprintf("no new block for over %s", w.maxHeadAge()))
			}
		}
	}
}

func (w *Watcher) onHead(ctx context.Context, h Head) {
	w.mu.Lock()
	w.lastHead = h
	snapshot := make(map[string]Grant, len(w.sessions))
	for id, s := range w.sessions {
		snapshot[id] = s.grant
	}
	w.mu.Unlock()

	type result struct {
		id  string
		v   Verdict
		err error
	}
	results := make(chan result, len(snapshot))
	var wg sync.WaitGroup
	for id, g := range snapshot {
		wg.Add(1)
		go func(id string, g Grant) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			v, err := w.Checker.CheckGrant(cctx, g, h.Number)
			results <- result{id, v, err}
		}(id, g)
	}
	wg.Wait()
	close(results)

	for r := range results {
		w.mu.Lock()
		s, ok := w.sessions[r.id]
		if !ok {
			w.mu.Unlock()
			continue
		}
		var rev *Revocation
		switch {
		case r.err != nil:
			s.errors++
			w.log().Warn("grant check failed", "session", r.id, "grant", s.grant.Name, "err", r.err, "consecutive", s.errors)
			if s.errors >= w.maxErrors() {
				rev = &Revocation{SessionID: r.id, Grant: s.grant.Name, Reason: "cannot verify grant on chain (fail closed)", Block: h.Number, HeadSeen: h.SeenAt}
			}
		case !r.v.Live:
			rev = &Revocation{SessionID: r.id, Grant: s.grant.Name, Reason: r.v.Reason, Block: h.Number, HeadSeen: h.SeenAt}
		default:
			s.errors = 0
		}
		if rev != nil {
			delete(w.sessions, r.id)
		}
		w.mu.Unlock()
		if rev != nil && w.OnRevoke != nil {
			w.OnRevoke(*rev)
		}
	}
}

func (w *Watcher) failClosed(reason string) {
	w.mu.Lock()
	revs := make([]Revocation, 0, len(w.sessions))
	for id, s := range w.sessions {
		revs = append(revs, Revocation{SessionID: id, Grant: s.grant.Name, Reason: reason + " (fail closed)", HeadSeen: time.Now()})
	}
	w.sessions = map[string]*watched{}
	w.mu.Unlock()
	if len(revs) > 0 {
		w.log().Error("failing closed: revoking all sessions", "reason", reason, "sessions", len(revs))
	}
	for _, r := range revs {
		if w.OnRevoke != nil {
			w.OnRevoke(r)
		}
	}
}
