package tui

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"

	"github.com/jashwanth0712/meshy/meshy/arkivkit"
)

const (
	DefaultAPI       = "https://api.meshy.live"
	DefaultRPC       = "https://ethereum-sepolia-rpc.publicnode.com"
	DefaultDashboard = "https://meshy.live"
)

// Options configure a dashboard run.
type Options struct {
	Mode      string // "daemon" (meshyd top) or "agent" (meshy top)
	Org       string
	Me        string // this device or agent, highlighted in the header
	API       string // meshy-sync base URL
	RPC       string // Sepolia JSON-RPC over HTTP(S)
	Arkiv     string // Arkiv RPC ("" disables)
	Control   string // meshyd control socket ("" for agent mode)
	Dashboard string

	Demo     bool          // canned, animated scenario (no network)
	At       time.Duration // demo: start this far into the loop
	JSON     bool          // print one State as JSON and exit
	Once     bool          // print one rendered frame and exit
	Width    int           // --once size (default: terminal or 120)
	Height   int
	ASCII    bool
	Color    bool // force truecolor (for captures piped to a file)
	NoSplash bool
	Out      io.Writer
}

func (o *Options) defaults() {
	if o.API == "" {
		o.API = DefaultAPI
	}
	if o.RPC == "" || !strings.HasPrefix(o.RPC, "http") {
		o.RPC = DefaultRPC // ws:// daemon URLs: the dashboard polls over HTTP
	}
	if o.Arkiv == "-" {
		o.Arkiv = ""
	} else if o.Arkiv == "" {
		o.Arkiv = arkivkit.RabdiRPC
	}
	if o.Dashboard == "" {
		o.Dashboard = DefaultDashboard
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
}

// Run shows the dashboard. Without a TTY on stdout (agents, pipes, CI) it
// prints the state as JSON instead, so `meshy top` is always safe to call.
func Run(ctx context.Context, o Options) error {
	o.defaults()
	g := Unicode
	if o.ASCII || os.Getenv("TERM") == "dumb" || os.Getenv("MESHY_ASCII") != "" {
		g = ASCII
	}
	if o.Color {
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
	}
	tty := term.IsTerminal(os.Stdout.Fd())
	if o.JSON || (!tty && !o.Once) {
		st := collect(ctx, o)
		enc := json.NewEncoder(o.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	if o.Once {
		w, h := o.Width, o.Height
		if tw, th, err := term.GetSize(os.Stdout.Fd()); err == nil && w == 0 {
			w, h = tw, th
		}
		if w == 0 {
			w = 120
		}
		if h == 0 {
			h = 36
		}
		st := collect(ctx, o)
		now := time.Now()
		if o.Demo {
			st = demoState(o.At, now)
		}
		f := &Frame{S: st, Now: now, W: w, H: h, G: g, Tick: int(o.At / (time.Second / fps)), Focus: 0, Sel: -1}
		fmt.Fprintln(o.Out, Render(f))
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, o, g)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err == tea.ErrProgramKilled {
		err = nil
	}
	return err
}

// collect does one synchronous round of every source (for --json / --once).
func collect(ctx context.Context, o Options) *State {
	if o.Demo {
		return demoState(o.At, time.Now())
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if o.Org == "" && o.Control != "" {
		if dr := fetchDaemon(ctx, o.Control); dr.Err == nil {
			o.Org = orgOf(dr.D.Resource)
		}
	}
	s := &State{Org: o.Org, Mode: o.Mode, Me: o.Me}
	var wg sync.WaitGroup
	var mr mapResult
	var hr headResult
	var dr daemonResult
	var fr feedResult
	if o.Org != "" {
		wg.Add(1)
		go func() { defer wg.Done(); mr = fetchMap(ctx, o.API, o.Org) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); hr = fetchHead(ctx, o.RPC) }()
	if o.Control != "" {
		wg.Add(1)
		go func() { defer wg.Done(); dr = fetchDaemon(ctx, o.Control) }()
	}
	if o.Arkiv != "" && o.Org != "" && o.Control == "" { // daemon mode: its own events are the feed
		wg.Add(1)
		go func() { defer wg.Done(); fr = fetchArkiv(ctx, o.Arkiv, o.Org) }()
	}
	wg.Wait()
	if mr.Err != nil {
		s.MapErr = mr.Err.Error()
	} else {
		s.Networks, s.Grants, s.MapBlock = mr.Networks, mr.Grants, mr.Block
	}
	if hr.Err != nil {
		s.HeadErr = hr.Err.Error()
	} else {
		s.setHead(hr.Num, hr.TS, hr.Txs, hr.Seen)
		s.TxBlocks = append(fetchTxHistory(ctx, o.RPC, hr.Num, 60), s.TxBlocks...)
	}
	if o.Control != "" {
		if dr.Err != nil {
			s.DaemonErr = dr.Err.Error()
		} else {
			s.Daemon = dr.D
			s.addEvents(dr.Recent)
			if s.Me == "" {
				s.Me = dr.D.Resource
			}
		}
	}
	if fr.Err != nil {
		s.FeedErr = fr.Err.Error()
	} else {
		s.addEvents(fr.Events)
	}
	return s
}

// Flags registers the shared dashboard flags on fs.
func (o *Options) Flags(fs *flag.FlagSet) {
	fs.StringVar(&o.Org, "org", os.Getenv("MESHY_ORG"), "org ENS name, e.g. meshydemo.eth")
	fs.StringVar(&o.API, "api", firstEnv("MESHY_API", "MESHY_PROVIDER"), "meshy-sync URL (default "+DefaultAPI+")")
	fs.StringVar(&o.RPC, "rpc", os.Getenv("MESHY_SEPOLIA"), "Sepolia HTTP RPC (default "+DefaultRPC+")")
	fs.StringVar(&o.Arkiv, "arkiv", os.Getenv("MESHY_ARKIV_RPC"), "Arkiv RPC (\"-\" disables)")
	fs.StringVar(&o.Dashboard, "dashboard", os.Getenv("MESHY_DASHBOARD"), "web dashboard URL (default "+DefaultDashboard+")")
	fs.BoolVar(&o.Demo, "demo", false, "play the canned revoke scenario (no network)")
	fs.DurationVar(&o.At, "at", 0, "demo: start this far into the 40 s loop (e.g. 21s = the revoke)")
	fs.BoolVar(&o.JSON, "json", false, "print the dashboard state as JSON and exit (default when stdout is not a TTY)")
	fs.BoolVar(&o.Once, "once", false, "render one frame to stdout and exit")
	fs.IntVar(&o.Width, "width", 0, "--once width")
	fs.IntVar(&o.Height, "height", 0, "--once height")
	fs.BoolVar(&o.ASCII, "ascii", false, "ASCII glyphs only (also TERM=dumb or MESHY_ASCII=1)")
	fs.BoolVar(&o.Color, "color", false, "force truecolor output (for captures)")
	fs.BoolVar(&o.NoSplash, "no-splash", os.Getenv("MESHY_NO_SPLASH") != "", "skip the logo animation")
}

func firstEnv(ks ...string) string {
	for _, k := range ks {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// PrintLogo writes the logo to w when it is a terminal (never into pipes,
// so machine-readable output stays clean).
func PrintLogo(w *os.File) {
	if !term.IsTerminal(w.Fd()) || os.Getenv("MESHY_NO_SPLASH") != "" {
		return
	}
	fmt.Fprintln(w, Logo(os.Getenv("TERM") == "dumb"))
	fmt.Fprintln(w)
}
