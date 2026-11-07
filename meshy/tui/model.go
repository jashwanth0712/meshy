package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

const (
	fps         = 10
	everyMap    = 10 * time.Second
	everyHead   = 3 * time.Second
	everyDaemon = time.Second
	everyArkiv  = 8 * time.Second
	splashFor   = 1600 * time.Millisecond
)

type (
	tickMsg time.Time
	mapMsg  struct {
		mapResult
		gen int
	}
	headMsg   headResult
	txHistMsg []float64
	daemonMsg daemonResult
	arkivMsg  struct {
		feedResult
		gen int
	}
	eventMsg audit.Event
	toastMsg string
)

type model struct {
	o       Options
	ctx     context.Context
	s       *State
	start   time.Time
	now     time.Time
	w, h    int
	g       Glyphs
	tick    int
	sel     int
	focus   int
	help    bool
	paused  bool
	toast   string
	toastT  time.Time
	confirm string // session id awaiting y/n
	events  chan audit.Event
	arkivOK bool // first Arkiv load done (older revokes do not flash)
	gen     int  // bumped when the org changes; stale map/feed loops stop
}

func newModel(ctx context.Context, o Options, g Glyphs) *model {
	now := time.Now()
	m := &model{o: o, ctx: ctx, g: g, start: now, now: now, w: o.Width, h: o.Height}
	if o.Demo {
		m.s = demoState(o.At, now)
	} else {
		m.s = &State{Org: o.Org, Mode: o.Mode, Me: o.Me}
	}
	if o.Control != "" && !o.Demo {
		m.events = make(chan audit.Event, 256)
		go followEvents(ctx, o.Control, m.events)
	}
	if o.Demo {
		m.focus = 0
	}
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second/fps, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func after(d time.Duration, f func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if d > 0 {
			time.Sleep(d)
		}
		return f()
	}
}

func (m *model) cmdMap(d time.Duration) tea.Cmd {
	gen, org := m.gen, m.o.Org
	return after(d, func() tea.Msg { return mapMsg{fetchMap(m.ctx, m.o.API, org), gen} })
}
func (m *model) cmdHead(d time.Duration) tea.Cmd {
	return after(d, func() tea.Msg { return headMsg(fetchHead(m.ctx, m.o.RPC)) })
}
func (m *model) cmdDaemon(d time.Duration) tea.Cmd {
	return after(d, func() tea.Msg { return daemonMsg(fetchDaemon(m.ctx, m.o.Control)) })
}
func (m *model) cmdArkiv(d time.Duration) tea.Cmd {
	gen, org := m.gen, m.o.Org
	return after(d, func() tea.Msg { return arkivMsg{fetchArkiv(m.ctx, m.o.Arkiv, org), gen} })
}

// switchOrg moves the dashboard to the next org this machine belongs to.
func (m *model) switchOrg() tea.Cmd {
	if len(m.o.Orgs) < 2 {
		m.say("this machine is in one org; use --org for another")
		return nil
	}
	i := 0
	for j, o := range m.o.Orgs {
		if o == m.o.Org {
			i = j + 1
		}
	}
	m.o.Org = m.o.Orgs[i%len(m.o.Orgs)]
	if m.o.MeFor != nil {
		m.o.Me = m.o.MeFor(m.o.Org)
	}
	m.gen++
	m.arkivOK = false
	m.s = &State{Org: m.o.Org, Mode: m.o.Mode, Me: m.o.Me}
	m.sel = 0
	m.say("org " + m.o.Org)
	cmds := []tea.Cmd{m.cmdMap(0)}
	if m.o.Arkiv != "" {
		cmds = append(cmds, m.cmdArkiv(0))
	}
	return tea.Batch(cmds...)
}
func (m *model) cmdEvent() tea.Cmd {
	return func() tea.Msg {
		select {
		case e := <-m.events:
			return eventMsg(e)
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	if m.o.Demo {
		return tea.Batch(cmds...)
	}
	if m.o.Org != "" {
		cmds = append(cmds, m.cmdMap(0))
	}
	cmds = append(cmds, m.cmdHead(0))
	if m.o.Control != "" {
		cmds = append(cmds, m.cmdDaemon(0), m.cmdEvent())
	}
	if m.o.Arkiv != "" && m.o.Org != "" {
		cmds = append(cmds, m.cmdArkiv(0))
	}
	return tea.Batch(cmds...)
}

func (m *model) flash(e audit.Event) {
	if e.TS.IsZero() || time.Since(e.TS) > 2*time.Minute {
		return
	}
	m.s.Flash = revokeFlash(e, m.s.Org)
	m.s.Flash.At = m.now
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		if !m.paused {
			m.tick++
			m.now = time.Time(msg)
			if m.o.Demo {
				m.s = demoState(m.now.Sub(m.start)+m.o.At, m.now)
			} else {
				m.s.tickOps(m.now)
			}
		}
		if m.toast != "" && time.Since(m.toastT) > 4*time.Second {
			m.toast = ""
		}
		return m, tick()
	case mapMsg:
		if msg.gen != m.gen {
			return m, nil // the org changed: this loop stops
		}
		if msg.Err != nil {
			m.s.MapErr = msg.Err.Error()
		} else {
			old := map[string]string{}
			for _, g := range m.s.Grants {
				old[g.Name] = g.Status
			}
			m.s.Networks, m.s.Grants, m.s.MapBlock, m.s.MapErr = msg.Networks, msg.Grants, msg.Block, ""
			for _, g := range msg.Grants {
				if old[g.Name] == "live" && g.Status == "revoked" && !m.s.Flash.Active(m.now) {
					m.flash(audit.Event{Kind: audit.SessionRevoke, Grant: g.Name, Block: msg.Block, TS: time.Now()})
				}
			}
		}
		return m, m.cmdMap(everyMap)
	case headMsg:
		if msg.Err != nil {
			m.s.HeadErr = msg.Err.Error()
		} else {
			m.s.HeadErr = ""
			first := m.s.Head == 0
			m.s.setHead(msg.Num, msg.TS, msg.Txs, msg.Seen)
			if first {
				head := msg.Num
				return m, tea.Batch(m.cmdHead(everyHead), func() tea.Msg { return txHistMsg(fetchTxHistory(m.ctx, m.o.RPC, head, 60)) })
			}
		}
		return m, m.cmdHead(everyHead)
	case txHistMsg:
		m.s.TxBlocks = append([]float64(msg), m.s.TxBlocks...)
	case daemonMsg:
		if msg.Err != nil {
			m.s.DaemonErr, m.s.Daemon = msg.Err.Error(), nil
		} else {
			m.s.DaemonErr, m.s.Daemon = "", msg.D
			m.s.addEvents(msg.Recent)
			if m.s.Me == "" {
				m.s.Me = msg.D.Resource
			}
			if m.o.Org == "" { // learn the org from the daemon's own name
				m.o.Org, m.s.Org = orgOf(msg.D.Resource), orgOf(msg.D.Resource)
				return m, tea.Batch(m.cmdDaemon(everyDaemon), m.cmdMap(0))
			}
		}
		return m, m.cmdDaemon(everyDaemon)
	case arkivMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if msg.Err != nil {
			m.s.FeedErr = msg.Err.Error()
		} else {
			m.s.FeedErr = ""
			for _, e := range m.s.addEvents(msg.Events) {
				if m.arkivOK && e.Kind == audit.SessionRevoke {
					m.flash(e)
				}
			}
			m.arkivOK = true
		}
		return m, m.cmdArkiv(everyArkiv)
	case eventMsg:
		e := audit.Event(msg)
		if len(m.s.addEvents([]audit.Event{e})) > 0 {
			if e.Kind == audit.Op && time.Since(e.TS) < 5*time.Second {
				m.s.countOp(m.now)
			}
			if e.Kind == audit.SessionRevoke {
				m.flash(e)
			}
		}
		return m, m.cmdEvent()
	case toastMsg:
		m.say(string(msg))
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) say(s string) { m.toast, m.toastT = s, time.Now() }

func (m *model) listLen() int {
	if m.focus == 0 {
		if m.s.Daemon == nil {
			return 0
		}
		return len(m.s.Daemon.Sessions)
	}
	return len(m.s.Grants)
}

func (m *model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.o.NoSplash && m.now.Sub(m.start) < splashFor && k.String() != "ctrl+c" {
		m.o.NoSplash = true // any key skips the splash
		return m, nil
	}
	if m.confirm != "" {
		id := m.confirm
		m.confirm = ""
		if k.String() == "y" {
			if m.o.Demo {
				m.say("demo: would kill " + id)
				return m, nil
			}
			return m, func() tea.Msg {
				if err := killSession(m.o.Control, id); err != nil {
					return toastMsg(err.Error())
				}
				return toastMsg("killed " + id)
			}
		}
		m.say("kill cancelled")
		return m, nil
	}
	switch k.String() {
	case "q", "ctrl+c", "esc":
		if m.help && k.String() == "esc" {
			m.help = false
			return m, nil
		}
		return m, tea.Quit
	case "?", "h":
		m.help = !m.help
	case "p", " ":
		m.paused = !m.paused
	case "a":
		if m.g.Braille {
			m.g = ASCII
		} else {
			m.g = Unicode
		}
	case "tab":
		m.focus, m.sel = 1-m.focus, 0
	case "up":
		m.sel = max(0, m.sel-1)
	case "down", "j":
		m.sel = min(max(0, m.listLen()-1), m.sel+1)
	case "k":
		if m.focus != 0 || m.s.Daemon == nil || m.sel >= len(m.s.Daemon.Sessions) {
			m.say("select a session first (tab)")
			return m, nil
		}
		m.confirm = m.s.Daemon.Sessions[m.sel].ID
		m.say("kill session " + m.confirm + "? y/n")
	case "c":
		dev := ""
		if m.focus == 1 && m.sel < len(m.s.Grants) {
			dev = m.s.Grants[m.sel].Device
		} else if m.s.Daemon != nil {
			dev = m.s.Daemon.Resource
		}
		if dev == "" {
			m.say("nothing selected")
			return m, nil
		}
		cmd := "meshy ssh " + dev + " -- "
		m.say(copyText(cmd) + ": " + cmd)
	case "g":
		return m, m.switchOrg()
	case "o":
		u := m.o.Dashboard + "/org/" + m.s.Org
		if os.Getenv("SSH_TTY") == "" && openURL(u) == nil {
			m.say("opened " + u)
		} else {
			m.say(u)
		}
	}
	return m, nil
}

// copyText uses the local clipboard when we are on the user's machine, and
// OSC 52 (terminal clipboard escape; works over SSH in iTerm2, kitty,
// WezTerm, tmux with set-clipboard) otherwise.
func copyText(s string) string {
	if os.Getenv("SSH_TTY") == "" && runtime.GOOS == "darwin" {
		c := exec.Command("pbcopy")
		if in, err := c.StdinPipe(); err == nil && c.Start() == nil {
			in.Write([]byte(s))
			in.Close()
			if c.Wait() == nil {
				return "copied"
			}
		}
	}
	fmt.Fprintf(os.Stderr, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(s)))
	return "copied (OSC 52)"
}

func openURL(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "linux":
		return exec.Command("xdg-open", u).Start()
	}
	return fmt.Errorf("unsupported")
}

func (m *model) frame() *Frame {
	f := &Frame{S: m.s, Now: m.now, W: m.w, H: m.h, G: m.g, Tick: m.tick, Sel: m.sel, Focus: m.focus,
		Help: m.help, Paused: m.paused, Status: m.toast, Orgs: len(m.o.Orgs)}
	if !m.o.NoSplash {
		if e := m.now.Sub(m.start); e < splashFor {
			f.Splash = max(0.01, float64(e)/float64(splashFor))
		}
	}
	return f
}

func (m *model) View() string { return Render(m.frame()) }
