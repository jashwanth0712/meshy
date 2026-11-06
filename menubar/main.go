// Command meshy-menubar is Meshy's macOS menu-bar app.
//
// It shows, at a glance: the org and chain status, this Mac's agent
// identities and their live grants (with countdowns), live sessions on a
// local meshyd, and recent blocks/revocations from Arkiv — with native
// notifications when an agent is blocked or a session is revoked.
//
// Data sources (all read-only):
//   - meshy-sync /v1/map  (org tree from ENSv2)       MESHY_SYNC   default http://localhost:8787
//   - Arkiv Rabdi         (audit entities)             MESHY_ARKIV  default public RPC
//   - meshyd control sock (local live sessions)        MESHY_CONTROL default /tmp/meshyd-live.sock
//   - ~/.meshy/agent*.json (agent identities on this Mac)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

var (
	org       = env("MESHY_ORG", "meshydemo.eth")
	syncURL   = env("MESHY_SYNC", "http://localhost:8787")
	dashboard = env("MESHY_DASHBOARD", "http://localhost:3000")
	arkivURL  = env("MESHY_ARKIV", "https://rpc.rabdi.db-chain.testnet.arkiv.network")
	control   = env("MESHY_CONTROL", "/tmp/meshyd-live.sock")
	httpc     = &http.Client{Timeout: 15 * time.Second}
)

// ---------- data ----------

type grant struct {
	Name, Agent, Resource, Status string
	Expiry                        int64
}

type snapshot struct {
	orgOK       bool
	orgErr      string
	block       uint64
	resources   []string
	agents      map[string]bool // label -> human-backed
	grants      []grant
	identities  []identity
	sessions    []session
	daemon      string
	daemonFresh bool
	events      []event
}

type identity struct {
	Agent string `json:"agent"`
	Org   string `json:"org"`
	Addr  string `json:"addr"`
	dir   string
}

type session struct {
	ID, Agent, Grant, Paths string
	Argv                    []string
	Expiry                  time.Time
}

type event struct {
	key, kind, agent, resource, reason string
	ts                                 int64
	block, latency                     int64
}

// loadMap reads the org tree from meshy-sync. It is lenient about shape so
// small API changes don't break the menu.
func loadMap(ctx context.Context, s *snapshot) {
	req, _ := http.NewRequestWithContext(ctx, "GET", syncURL+"/v1/map?org="+org, nil)
	resp, err := httpc.Do(req)
	if err != nil {
		s.orgErr = "provider unreachable"
		return
	}
	defer resp.Body.Close()
	var m map[string]any
	if json.NewDecoder(resp.Body).Decode(&m) != nil || resp.StatusCode != 200 {
		s.orgErr = fmt.Sprintf("provider error %d", resp.StatusCode)
		return
	}
	s.orgOK = true
	s.block = uint64(num(m["block"]))
	s.agents = map[string]bool{}
	for _, a := range list(m["agents"]) {
		am := obj(a)
		label := str(am["name"])
		if fn := str(am["fullName"]); fn != "" {
			label = strings.SplitN(fn, ".", 2)[0]
		}
		s.agents[label] = truthy(am["worldIdValid"])
	}
	for _, n := range list(m["networks"]) {
		for _, r := range list(obj(n)["resources"]) {
			rm := obj(r)
			rn := str(rm["fullName"])
			if rn == "" {
				rn = str(rm["name"])
			}
			s.resources = append(s.resources, rn)
		}
	}
	// meshy-sync lists grants flat: fullName = <agent>.<target>. Keep only
	// those whose first label is a known agent (children of a device registry
	// can also be services).
	for _, g := range list(m["grants"]) {
		gm := obj(g)
		full := str(gm["fullName"])
		parts := strings.SplitN(full, ".", 2)
		if len(parts) != 2 {
			continue
		}
		if _, isAgent := s.agents[parts[0]]; !isAgent {
			continue
		}
		s.grants = append(s.grants, grant{Name: full, Agent: parts[0], Resource: parts[1], Status: str(gm["status"]), Expiry: num(gm["expiry"])})
	}
}

func loadIdentities(s *snapshot) {
	h, _ := os.UserHomeDir()
	paths, _ := filepath.Glob(filepath.Join(h, ".meshy", "agent*", "agent.json"))
	if p := filepath.Join(h, ".meshy", "agent.json"); exists(p) {
		paths = append(paths, p)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var id identity
		if json.Unmarshal(b, &id) == nil && id.Agent != "" {
			id.dir = filepath.Dir(p)
			s.identities = append(s.identities, id)
		}
	}
}

func loadDaemon(ctx context.Context, s *snapshot) {
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", control)
	}}}
	resp, err := c.Get("http://meshyd/status")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var st struct {
		Resource string `json:"resource"`
		Healthy  bool   `json:"healthy"`
		Sessions []struct {
			ID, Agent, Grant, Paths string
			Argv                    []string
			Expiry                  time.Time
		} `json:"sessions"`
	}
	if json.NewDecoder(resp.Body).Decode(&st) != nil {
		return
	}
	s.daemon, s.daemonFresh = st.Resource, st.Healthy
	for _, x := range st.Sessions {
		s.sessions = append(s.sessions, session{x.ID, x.Agent, x.Grant, x.Paths, x.Argv, x.Expiry})
	}
}

func loadEvents(ctx context.Context, s *snapshot) {
	q := fmt.Sprintf(`(arkiv.schema = str('meshy.blocked') OR arkiv.schema = str('meshy.session')) AND org = str('%s') ORDER BY ts:u64 DESC`, org)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "arkiv_query", "params": []any{q, map[string]any{"select": map[string]any{"key": true, "attributes": true}, "limit": "0x14"}}})
	req, _ := http.NewRequestWithContext(ctx, "POST", arkivURL, strings.NewReader(string(body)))
	req.Header.Set("content-type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	var items []struct {
		Key        string `json:"key"`
		Attributes []struct {
			Name, Type string
			Value      any
		} `json:"attributes"`
	}
	if json.Unmarshal(r.Result, &items) != nil {
		var wrapped struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(r.Result, &wrapped) == nil {
			json.Unmarshal(wrapped.Data, &items)
		}
	}
	for _, it := range items {
		e := event{key: it.Key}
		for _, a := range it.Attributes {
			v := fmt.Sprint(a.Value)
			switch a.Name {
			case "kind":
				e.kind = v
			case "agent":
				e.agent = v
			case "resource":
				e.resource = v
			case "reason":
				e.reason = v
			case "ts":
				e.ts = hexNum(v)
			case "block":
				e.block = hexNum(v)
			case "latencyMs":
				e.latency = hexNum(v)
			}
		}
		if e.kind == "blocked" || e.kind == "denied" || e.kind == "session.revoke" {
			s.events = append(s.events, e)
		}
	}
	sort.Slice(s.events, func(i, j int) bool { return s.events[i].ts > s.events[j].ts })
}

// ---------- menu ----------

const (
	nGrants   = 8
	nSessions = 4
	nEvents   = 4
)

type ui struct {
	status, chain, daemon        *systray.MenuItem
	idHeader                     *systray.MenuItem
	ids                          [3]*systray.MenuItem
	grantHeader                  *systray.MenuItem
	grants                       [nGrants]*systray.MenuItem
	grantCmd                     [nGrants]string
	sessHeader                   *systray.MenuItem
	sessions                     [nSessions]*systray.MenuItem
	evHeader                     *systray.MenuItem
	events                       [nEvents]*systray.MenuItem
	openDash, openActivity, quit *systray.MenuItem
	refresh                      *systray.MenuItem

	mu       sync.Mutex
	seen     map[string]bool
	primed   bool
	snapshot snapshot
}

func main() { systray.Run(onReady, func() {}) }

func onReady() {
	systray.SetTitle("⬡ Meshy")
	systray.SetTooltip("Meshy — ENS-native mesh access")
	u := &ui{seen: map[string]bool{}}
	u.status = systray.AddMenuItem("Meshy · "+org, "")
	u.status.Disable()
	u.chain = systray.AddMenuItem("Connecting to provider…", "")
	u.chain.Disable()
	u.daemon = systray.AddMenuItem("", "")
	u.daemon.Disable()
	u.daemon.Hide()
	systray.AddSeparator()
	u.idHeader = systray.AddMenuItem("AGENTS ON THIS MAC", "")
	u.idHeader.Disable()
	for i := range u.ids {
		u.ids[i] = systray.AddMenuItem("", "")
		u.ids[i].Disable()
		u.ids[i].Hide()
	}
	systray.AddSeparator()
	u.grantHeader = systray.AddMenuItem("GRANTS (click to copy the meshy ssh command)", "")
	u.grantHeader.Disable()
	for i := range u.grants {
		u.grants[i] = systray.AddMenuItem("", "")
		u.grants[i].Hide()
		i := i
		go func() {
			for range u.grants[i].ClickedCh {
				u.mu.Lock()
				cmd := u.grantCmd[i]
				u.mu.Unlock()
				if cmd != "" {
					copyToClipboard(cmd)
					notify("Copied", cmd)
				}
			}
		}()
	}
	systray.AddSeparator()
	u.sessHeader = systray.AddMenuItem("LIVE SESSIONS ON THIS MAC'S DAEMON", "")
	u.sessHeader.Disable()
	u.sessHeader.Hide()
	for i := range u.sessions {
		u.sessions[i] = systray.AddMenuItem("", "")
		u.sessions[i].Disable()
		u.sessions[i].Hide()
	}
	u.evHeader = systray.AddMenuItem("RECENT BLOCKS & REVOCATIONS (Arkiv)", "")
	u.evHeader.Disable()
	for i := range u.events {
		u.events[i] = systray.AddMenuItem("", "")
		u.events[i].Disable()
		u.events[i].Hide()
	}
	systray.AddSeparator()
	u.openDash = systray.AddMenuItem("Open dashboard", "")
	u.openActivity = systray.AddMenuItem("Open activity log", "")
	u.refresh = systray.AddMenuItem("Refresh now", "")
	u.quit = systray.AddMenuItem("Quit Meshy", "")

	kick := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-u.openDash.ClickedCh:
				exec.Command("open", dashboard+"/org/"+org).Start()
			case <-u.openActivity.ClickedCh:
				exec.Command("open", dashboard+"/org/"+org+"/activity").Start()
			case <-u.refresh.ClickedCh:
				select {
				case kick <- struct{}{}:
				default:
				}
			case <-u.quit.ClickedCh:
				systray.Quit()
			}
		}
	}()
	go func() {
		t := time.NewTicker(5 * time.Second)
		for {
			u.tick()
			select {
			case <-t.C:
			case <-kick:
			}
		}
	}()
	// Countdown repaint every second without refetching.
	go func() {
		for range time.Tick(time.Second) {
			u.mu.Lock()
			s := u.snapshot
			u.mu.Unlock()
			u.paintGrants(s)
		}
	}()
}

func (u *ui) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var s snapshot
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); loadMap(ctx, &s) }()
	go func() { defer wg.Done(); loadDaemon(ctx, &s) }()
	var evs snapshot
	go func() { defer wg.Done(); loadEvents(ctx, &evs) }()
	loadIdentities(&s)
	wg.Wait()
	s.events = evs.events
	u.mu.Lock()
	u.snapshot = s
	u.mu.Unlock()
	u.paint(s)
}

func (u *ui) paint(s snapshot) {
	if s.orgOK {
		u.chain.SetTitle(fmt.Sprintf("● Sepolia #%d · %d resources · %d agents", s.block, len(s.resources), len(s.agents)))
	} else {
		u.chain.SetTitle("○ " + s.orgErr + " (" + syncURL + ")")
	}
	if s.daemon != "" {
		fresh := "chain fresh"
		if !s.daemonFresh {
			fresh = "CHAIN STALE — failing closed"
		}
		u.daemon.SetTitle(fmt.Sprintf("This Mac serves %s · %s", s.daemon, fresh))
		u.daemon.Show()
	} else {
		u.daemon.Hide()
	}
	for i, it := range u.ids {
		if i < len(s.identities) {
			id := s.identities[i]
			name := id.Agent + ".agents." + id.Org
			state := "not approved yet"
			if hb, ok := s.agents[id.Agent]; ok && id.Org == org {
				state = "active"
				if hb {
					state = "active · human-backed"
				}
			}
			it.SetTitle(fmt.Sprintf("🤖 %s — %s", name, state))
			it.Show()
		} else {
			it.Hide()
		}
	}
	if len(s.identities) == 0 {
		u.ids[0].SetTitle("No agent identity (meshy join <invite>)")
		u.ids[0].Show()
	}
	u.paintGrants(s)
	if len(s.sessions) > 0 {
		u.sessHeader.Show()
	} else {
		u.sessHeader.Hide()
	}
	for i, it := range u.sessions {
		if i < len(s.sessions) {
			x := s.sessions[i]
			it.SetTitle(fmt.Sprintf("● %s · %s · %s left · %s", x.Agent, shortCmd(x.Argv), left(x.Expiry.Unix()), x.Paths))
			it.Show()
		} else {
			it.Hide()
		}
	}
	for i, it := range u.events {
		if i < len(s.events) {
			e := s.events[i]
			it.SetTitle(describe(e))
			it.Show()
		} else {
			it.Hide()
		}
	}
	// Notifications for new blocks/revocations (skip the first load).
	u.mu.Lock()
	for _, e := range s.events {
		if !u.seen[e.key] {
			u.seen[e.key] = true
			if u.primed {
				notify("Meshy", describe(e))
			}
		}
	}
	u.primed = true
	u.mu.Unlock()
}

func (u *ui) paintGrants(s snapshot) {
	now := time.Now().Unix()
	mine := map[string]bool{}
	for _, id := range s.identities {
		mine[id.Agent] = true
	}
	var gs []grant
	for _, g := range s.grants {
		if g.Status == "live" && g.Expiry > now {
			gs = append(gs, g)
		}
	}
	sort.Slice(gs, func(i, j int) bool { return mine[gs[i].Agent] && !mine[gs[j].Agent] })
	live := 0
	u.mu.Lock()
	defer u.mu.Unlock()
	for i, it := range u.grants {
		if i < len(gs) {
			g := gs[i]
			live++
			tag := ""
			if mine[g.Agent] {
				tag = " (this Mac)"
			}
			it.SetTitle(fmt.Sprintf("🔑 %s → %s · %s left%s", g.Agent, strings.SplitN(g.Resource, ".", 2)[0], left(g.Expiry), tag))
			u.grantCmd[i] = "meshy ssh " + g.Resource + " -- "
			it.Show()
		} else {
			u.grantCmd[i] = ""
			it.Hide()
		}
	}
	if len(gs) == 0 {
		u.grants[0].SetTitle("No live grants")
		u.grantCmd[0] = ""
		u.grants[0].Show()
	}
	if live > 0 {
		systray.SetTitle(fmt.Sprintf("⬡ %d", live))
	} else {
		systray.SetTitle("⬡ Meshy")
	}
}

// ---------- helpers ----------

func describe(e event) string {
	when := time.UnixMilli(e.ts).Format("15:04")
	res := strings.SplitN(e.resource, ".", 2)[0]
	switch e.kind {
	case "session.revoke":
		return fmt.Sprintf("⚡ %s %s's session on %s killed (block %d, %dms)", when, e.agent, res, e.block, e.latency)
	case "denied":
		return fmt.Sprintf("⛔ %s %s denied on %s", when, e.agent, res)
	default:
		return fmt.Sprintf("⛔ %s %s blocked on %s (%s)", when, e.agent, res, e.reason)
	}
}

func left(exp int64) string {
	d := time.Until(time.Unix(exp, 0)).Round(time.Second)
	if d <= 0 {
		return "0s"
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func shortCmd(argv []string) string {
	if len(argv) >= 3 && argv[1] == "-c" {
		argv = argv[2:]
	}
	s := strings.Join(argv, " ")
	if len(s) > 32 {
		s = s[:32] + "…"
	}
	return s
}

func notify(title, msg string) {
	q := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
	exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s"`, q(msg), q(title))).Start()
}

func copyToClipboard(s string) {
	c := exec.Command("pbcopy")
	c.Stdin = strings.NewReader(s)
	c.Run()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func truthy(v any) bool { b, _ := v.(bool); return b }

func num(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		return hexNum(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}

func hexNum(s string) int64 {
	if strings.HasPrefix(s, "0x") {
		n, _ := strconv.ParseInt(s[2:], 16, 64)
		return n
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
