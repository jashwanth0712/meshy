package tui

import (
	"fmt"
	"time"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

// Demo mode: a canned, deterministic 40 s loop so screenshots and judges see
// the whole story without a chain: agents working → a blocked write → the
// admin revokes claude-1's grant on db1 → its session dies one block later →
// a fresh grant brings it back.

const (
	demoLoop      = 40 * time.Second
	demoBlocked   = 9 * time.Second
	demoRevoke    = 20 * time.Second
	demoRegrant   = 31 * time.Second
	demoBlockTime = 6 * time.Second // faster than Sepolia's 12 s, for the camera
	demoOrg       = "meshydemo.eth"
	demoHead0     = 12076883
)

// demoState is the dashboard state t into the loop (pure: same t, same frame).
func demoState(t time.Duration, now time.Time) *State {
	t = t % demoLoop
	start := now.Add(-t)
	s := &State{Org: demoOrg, Mode: "demo", Me: "db1.prod." + demoOrg}
	s.Networks = []Network{
		{Name: "prod", Full: "prod." + demoOrg, Devices: []Device{
			{Name: "db1", Full: "db1.prod." + demoOrg, Kind: "server"},
			{Name: "api", Full: "api.prod." + demoOrg, Kind: "server", Services: []Service{{Name: "grafana", Full: "grafana.api.prod." + demoOrg}}},
		}},
		{Name: "home", Full: "home." + demoOrg, Devices: []Device{
			{Name: "pi", Full: "pi.home." + demoOrg, Kind: "raspberry-pi", Services: []Service{{Name: "camera", Full: "camera.pi.home." + demoOrg}}},
			{Name: "rig1", Full: "rig1.home." + demoOrg, Kind: "workstation"},
		}},
	}
	a := func(l string) string { return l + ".agents." + demoOrg }
	db1 := "db1.prod." + demoOrg
	cam := "camera.pi.home." + demoOrg
	rig := "rig1.home." + demoOrg
	graf := "grafana.api.prod." + demoOrg
	g1 := Grant{Name: "claude-1." + db1, Agent: a("claude-1"), Target: db1, Device: db1, Status: "live", Kind: "device", Expiry: start.Add(14 * time.Minute)}
	s.Grants = []Grant{
		g1,
		{Name: "claude-1." + cam, Agent: a("claude-1"), Target: cam, Device: "pi.home." + demoOrg, Status: "live", Kind: "service", Expiry: start.Add(48 * time.Second)},
		{Name: "codex-2." + rig, Agent: a("codex-2"), Target: rig, Device: rig, Status: "live", Kind: "device", Expiry: start.Add(41 * time.Minute)},
		{Name: "gemini-3." + graf, Agent: a("gemini-3"), Target: graf, Device: "api.prod." + demoOrg, Status: "expired", Kind: "service", Expiry: start.Add(-3 * time.Minute)},
	}
	if s.Grants[1].Expiry.Before(now) {
		s.Grants[1].Status = "expired"
	}
	if t >= demoRevoke && t < demoRegrant {
		s.Grants[0].Status = "revoked"
	}
	if t >= demoRegrant {
		s.Grants[0].Expiry = start.Add(demoRegrant + 10*time.Minute)
	}

	// Chain: a block every demoBlockTime; tx counts from a fixed pattern.
	nBlocks := int(t / demoBlockTime)
	txPat := []float64{142, 97, 188, 121, 64, 203, 156, 88, 131, 175, 109, 92, 168, 144, 77}
	for i := 0; i <= nBlocks+80; i++ {
		s.TxBlocks = append(s.TxBlocks, txPat[(i+nBlocks)%len(txPat)])
	}
	s.TxBlocks = s.TxBlocks[len(s.TxBlocks)-80:]
	s.Head = demoHead0 + uint64(nBlocks)
	s.HeadTime = start.Add(time.Duration(nBlocks) * demoBlockTime)
	s.HeadSeen = s.HeadTime.Add(900 * time.Millisecond)
	if nBlocks > 0 {
		s.headPrev, s.headBump = s.Head-1, s.HeadSeen
	}
	revokeBlock := demoHead0 + uint64(demoRevoke/demoBlockTime)

	// Sessions.
	sess1 := Session{ID: "93b205a3b873", Agent: "claude-1", Grant: g1.Name, Argv: []string{"psql", "-c", "select count(*) from orders"},
		Started: start.Add(-2 * time.Minute), Expiry: g1.Expiry, Paths: "/srv/data:rw /srv/logs:ro"}
	sess2 := Session{ID: "5d1e09c4aa20", Agent: "codex-2", Grant: s.Grants[2].Name, Argv: []string{"make", "test"},
		Started: start.Add(-5 * time.Minute), Expiry: s.Grants[2].Expiry, Paths: "/home/dev/src:rw"}
	sess3 := Session{ID: "c0ffee5e5510", Agent: "claude-1", Grant: g1.Name, Argv: []string{"pg_dump", "--schema-only"},
		Started: start.Add(demoRegrant + 2*time.Second), Expiry: s.Grants[0].Expiry, Paths: "/srv/data:rw"}
	d := &Daemon{Resource: db1, Addr: "0x7Dec34A86907eeF26c70A6578df7bAaE7F223384", Sandbox: "fuse", Healthy: true,
		HeadNumber: s.Head, HeadAgeSec: now.Sub(s.HeadTime).Seconds()}
	if t < demoRevoke {
		d.Sessions = append(d.Sessions, sess1)
	}
	d.Sessions = append(d.Sessions, sess2)
	if t >= demoRegrant+2*time.Second {
		d.Sessions = append(d.Sessions, sess3)
	}
	s.Daemon = d

	// Feed + ops/s: generate events for [0, t].
	files := []string{"/srv/data/orders.csv", "/srv/data/users.parquet", "/srv/logs/pg.log", "/srv/data/schema.sql", "/srv/data/idx/orders_pk"}
	ops := []string{"read", "open", "read", "readdir", "write", "read"}
	var evs []audit.Event
	evs = append(evs,
		audit.Event{Kind: audit.SessionStart, TS: sess2.Started, Agent: "codex-2", Grant: sess2.Grant, Session: sess2.ID, Resource: rig},
		audit.Event{Kind: audit.SessionStart, TS: sess1.Started, Agent: "claude-1", Grant: g1.Name, Session: sess1.ID, Resource: db1})
	secs := int(t / time.Second)
	opsPerSec := make([]float64, 0, 60)
	for i := -40; i <= secs; i++ {
		n := 0
		if i < int(demoRevoke/time.Second) || i >= int((demoRegrant+2*time.Second)/time.Second) {
			n = 2 + pm(i*7+3, 5) + pm(i*i, 3)
		}
		if i == secs { // the current second is still filling
			n = n * int(t%time.Second) / int(time.Second)
		}
		for k := 0; k < n; k++ {
			ts := start.Add(time.Duration(i)*time.Second + time.Duration(k)*time.Second/time.Duration(n+1))
			if i < 0 && k > 0 {
				continue // keep pre-roll short in the feed
			}
			sid := sess1.ID
			if i >= int(demoRegrant/time.Second) {
				sid = sess3.ID
			}
			evs = append(evs, audit.Event{Kind: audit.Op, TS: ts, Agent: "claude-1", Grant: g1.Name, Session: sid, Resource: db1,
				Op: ops[pm(i+k, len(ops))], Path: files[pm(i*3+k, len(files))], Bytes: int64(512 + pm(i*k*977, 40000))})
		}
		if i < secs {
			opsPerSec = append(opsPerSec, float64(n))
		}
	}
	s.OpsPerSec = opsPerSec
	if t >= demoBlocked {
		evs = append(evs, audit.Event{Kind: audit.Blocked, TS: start.Add(demoBlocked), Agent: "claude-1", Grant: g1.Name, Session: sess1.ID,
			Resource: db1, Op: "write", Path: "/etc/passwd", Reason: "outside grant"})
	}
	if t >= demoRevoke {
		e := audit.Event{Kind: audit.SessionRevoke, TS: start.Add(demoRevoke), Agent: "claude-1", Grant: g1.Name,
			Session: sess1.ID, Resource: db1, Block: revokeBlock, Latency: 1316, Reason: "revoked or expired (grant or an ancestor is no longer registered)"}
		evs = append(evs, e)
		if t < demoRevoke+8*time.Second {
			s.Flash = revokeFlash(e, demoOrg)
		}
	}
	if t >= demoRegrant+2*time.Second {
		evs = append(evs, audit.Event{Kind: audit.SessionStart, TS: sess3.Started, Agent: "claude-1", Grant: g1.Name, Session: sess3.ID, Resource: db1})
	}
	if t >= 26*time.Second {
		evs = append(evs, audit.Event{Kind: audit.Denied, TS: start.Add(26 * time.Second), Agent: "claude-1", Resource: db1,
			Reason: fmt.Sprintf("MESHY_NO_GRANT resource=%s", db1)})
	}
	s.addEvents(evs)
	return s
}

// pm is a modulo that stays non-negative.
func pm(a, n int) int { return ((a % n) + n) % n }
