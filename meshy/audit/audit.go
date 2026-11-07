// Package audit defines the events meshyd records: session lifecycle, every
// file operation inside a grant, and every blocked attempt. Sinks fan them
// out to a local JSONL file, an in-memory ring (for `meshyd status`), and
// Arkiv (arkivkit).
package audit

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Kind classifies an event.
type Kind string

const (
	SessionStart  Kind = "session.start"
	SessionEnd    Kind = "session.end"
	SessionRevoke Kind = "session.revoke"
	Op            Kind = "op"      // allowed file operation
	Blocked       Kind = "blocked" // denied file operation or command
	Denied        Kind = "denied"  // connection refused at auth
)

// Event is one audit record.
type Event struct {
	Kind        Kind      `json:"kind"`
	TS          time.Time `json:"ts"`
	Resource    string    `json:"resource"`
	Agent       string    `json:"agent,omitempty"`   // agent label, e.g. claude-1
	Grant       string    `json:"grant,omitempty"`   // full grant name
	Session     string    `json:"session,omitempty"` // session id
	Op          string    `json:"op,omitempty"`      // open, read, write, create, unlink, rename, readdir, exec, ...
	Path        string    `json:"path,omitempty"`
	Bytes       int64     `json:"bytes,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Block       uint64    `json:"block,omitempty"`     // chain block that triggered a revoke
	Latency     float64   `json:"latencyMs,omitempty"` // revoke: head seen -> session killed
	Argv        []string  `json:"argv,omitempty"`
	DurationMs  int64     `json:"durationMs,omitempty"`  // session.end: how long the session ran
	GrantExpiry time.Time `json:"grantExpiry,omitempty"` // set on SessionStart; drives per-session Arkiv lifetime
}

// Sink receives events. Implementations must not block for long.
type Sink interface{ Emit(Event) }

// SinkFunc adapts a function to Sink.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }

// Multi fans out to several sinks.
type Multi []Sink

func (m Multi) Emit(e Event) {
	for _, s := range m {
		if s != nil {
			s.Emit(e)
		}
	}
}

// JSONL writes one JSON object per line.
type JSONL struct {
	mu sync.Mutex
	W  io.Writer
}

func (j *JSONL) Emit(e Event) {
	b, _ := json.Marshal(e)
	j.mu.Lock()
	defer j.mu.Unlock()
	j.W.Write(append(b, '\n'))
}

// Ring keeps the last N events in memory.
type Ring struct {
	mu   sync.Mutex
	N    int
	buf  []Event
	subs []chan Event
}

func (r *Ring) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.N
	if n == 0 {
		n = 500
	}
	r.buf = append(r.buf, e)
	if len(r.buf) > n {
		r.buf = r.buf[len(r.buf)-n:]
	}
	for _, s := range r.subs {
		select {
		case s <- e:
		default:
		}
	}
}

// Snapshot returns a copy of the buffered events, oldest first.
func (r *Ring) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.buf...)
}

// Subscribe returns a channel of future events (lossy if the reader lags).
func (r *Ring) Subscribe() <-chan Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan Event, 64)
	r.subs = append(r.subs, ch)
	return ch
}
