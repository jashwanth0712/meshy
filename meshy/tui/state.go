package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

// State is everything the dashboard shows. It is also the --json output, so
// field names are part of the agent-facing contract.
type State struct {
	Org      string    `json:"org"`
	Mode     string    `json:"mode"` // daemon | agent | demo
	Me       string    `json:"me,omitempty"`
	Networks []Network `json:"networks"`
	Grants   []Grant   `json:"grants"`
	MapBlock uint64    `json:"mapBlock,omitempty"`
	MapErr   string    `json:"mapErr,omitempty"`

	Head     uint64    `json:"head"`
	HeadTime time.Time `json:"headTime"` // block timestamp
	HeadSeen time.Time `json:"headSeen"` // when we saw it
	HeadErr  string    `json:"headErr,omitempty"`
	TxBlocks []float64 `json:"txPerBlock,omitempty"`

	Daemon    *Daemon `json:"daemon,omitempty"`
	DaemonErr string  `json:"daemonErr,omitempty"`

	Feed      []audit.Event `json:"feed"` // newest first
	FeedErr   string        `json:"feedErr,omitempty"`
	OpsPerSec []float64     `json:"opsPerSec,omitempty"` // oldest first, 1 s buckets

	Flash *Flash `json:"flash,omitempty"`

	seen      map[string]bool
	opsBucket int64
	opsCount  float64
	headPrev  uint64
	headBump  time.Time
}

type Network struct {
	Name    string   `json:"name"`
	Full    string   `json:"fullName"`
	Devices []Device `json:"devices"`
}

type Device struct {
	Name     string    `json:"name"`
	Full     string    `json:"fullName"`
	Kind     string    `json:"kind,omitempty"`
	Services []Service `json:"services,omitempty"`
}

type Service struct {
	Name string `json:"name"`
	Full string `json:"fullName"`
}

type Grant struct {
	Name   string    `json:"fullName"`
	Agent  string    `json:"agent"`  // claude-1.agents.org
	Target string    `json:"target"` // device or service
	Device string    `json:"device"` // what the agent connects to
	Status string    `json:"status"` // live | revoked | expired
	Kind   string    `json:"kind"`
	Expiry time.Time `json:"expiry"`
}

// Daemon mirrors meshyd's GET /status (cmd/meshyd/control.go).
type Daemon struct {
	Resource   string    `json:"resource"`
	Addr       string    `json:"addr"`
	Sandbox    string    `json:"sandbox"`
	Healthy    bool      `json:"healthy"`
	HeadNumber uint64    `json:"headNumber"`
	HeadAgeSec float64   `json:"headAgeSec"`
	Sessions   []Session `json:"sessions"`
}

// Session mirrors meshyd.SessionInfo (no json tags there, so Go field names).
type Session struct {
	ID      string
	Agent   string
	Grant   string
	Argv    []string
	Started time.Time
	Expiry  time.Time
	Paths   string
}

// Flash is the red revoke banner.
type Flash struct {
	Text    string    `json:"text"`
	Grant   string    `json:"grant"`
	Agent   string    `json:"agent,omitempty"`
	Session string    `json:"session,omitempty"`
	Block   uint64    `json:"block,omitempty"`
	MS      float64   `json:"latencyMs,omitempty"`
	At      time.Time `json:"at"`
}

func (f *Flash) Active(now time.Time) bool { return f != nil && now.Sub(f.At) < 8*time.Second }

func label(full string) string {
	if i := strings.IndexByte(full, '.'); i > 0 {
		return full[:i]
	}
	return full
}

// short drops the org suffix: db1.prod.meshydemo.eth → db1.prod.
func short(full, org string) string {
	return strings.TrimSuffix(strings.TrimSuffix(full, org), ".")
}

func orgOf(name string) string {
	p := strings.Split(name, ".")
	if len(p) < 2 {
		return name
	}
	return strings.Join(p[len(p)-2:], ".")
}

func eventKey(e audit.Event) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s", e.Kind, e.TS.UnixNano(), e.Session, e.Op, e.Path, e.Grant)
}

const feedCap = 200

// addEvents merges events (any order) into the feed, newest first. It
// returns the ones that were new.
func (s *State) addEvents(evs []audit.Event) []audit.Event {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	var fresh []audit.Event
	for _, e := range evs {
		k := eventKey(e)
		if s.seen[k] {
			continue
		}
		s.seen[k] = true
		fresh = append(fresh, e)
	}
	if len(fresh) == 0 {
		return nil
	}
	s.Feed = append(s.Feed, fresh...)
	sort.SliceStable(s.Feed, func(i, j int) bool { return s.Feed[i].TS.After(s.Feed[j].TS) })
	if len(s.Feed) > feedCap {
		for _, e := range s.Feed[feedCap:] {
			delete(s.seen, eventKey(e))
		}
		s.Feed = s.Feed[:feedCap]
	}
	return fresh
}

// countOp feeds the ops/s sparkline; tickOps closes 1 s buckets.
func (s *State) countOp(now time.Time) {
	s.tickOps(now)
	s.opsCount++
}

func (s *State) tickOps(now time.Time) {
	b := now.Unix()
	if s.opsBucket == 0 {
		s.opsBucket = b
	}
	for s.opsBucket < b {
		s.OpsPerSec = append(s.OpsPerSec, s.opsCount)
		s.opsCount = 0
		s.opsBucket++
		if len(s.OpsPerSec) > 120 {
			s.OpsPerSec = s.OpsPerSec[len(s.OpsPerSec)-120:]
		}
	}
}

func (s *State) setHead(num uint64, ts time.Time, txs int, now time.Time) {
	if num == s.Head {
		return
	}
	if s.Head != 0 && num > s.Head {
		s.headPrev, s.headBump = s.Head, now
	}
	s.Head, s.HeadTime, s.HeadSeen = num, ts, now
	s.TxBlocks = append(s.TxBlocks, float64(txs))
	if len(s.TxBlocks) > 120 {
		s.TxBlocks = s.TxBlocks[len(s.TxBlocks)-120:]
	}
}

// revokeFlash builds the banner for a revoke event.
func revokeFlash(e audit.Event, org string) *Flash {
	t := fmt.Sprintf("REVOKED %s", short(e.Grant, org))
	if e.Block != 0 {
		t += fmt.Sprintf(" · block #%d", e.Block)
	}
	if e.Latency > 0 {
		t += fmt.Sprintf(" · session killed %.0f ms after head", e.Latency)
	}
	return &Flash{Text: t, Grant: e.Grant, Agent: e.Agent, Session: e.Session, Block: e.Block, MS: e.Latency, At: e.TS}
}

// sessionsFor returns live sessions whose grant points at target.
func (s *State) sessionsFor(target string) []Session {
	if s.Daemon == nil {
		return nil
	}
	var out []Session
	for _, se := range s.Daemon.Sessions {
		if strings.HasSuffix(se.Grant, "."+target) {
			out = append(out, se)
		}
	}
	return out
}

func (s *State) grantsFor(target string) []Grant {
	var out []Grant
	for _, g := range s.Grants {
		if g.Target == target {
			out = append(out, g)
		}
	}
	return out
}

// activeTargets: grant targets with recent session/op activity (feed, last 15s).
func (s *State) recentActivity(target string, now time.Time) bool {
	for _, e := range s.Feed {
		if now.Sub(e.TS) > 15*time.Second {
			break
		}
		if (e.Kind == audit.Op || e.Kind == audit.SessionStart) && strings.HasSuffix(e.Grant, "."+target) {
			return true
		}
	}
	return false
}
