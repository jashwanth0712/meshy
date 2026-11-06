package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jashwanth0712/meshy/meshy/audit"
)

// Frame holds everything one render needs. Render is a pure function of it,
// which is what makes --once / --demo --at captures reproducible.
type Frame struct {
	S      *State
	Now    time.Time
	W, H   int
	G      Glyphs
	Tick   int // animation frame, 10 per second
	Sel    int // selected row in the focused list
	Focus  int // 0 sessions, 1 grants
	Help   bool
	Paused bool
	Status string  // one-line toast (copied, killed, …)
	Splash float64 // 0..1 while the splash plays, else 0
}

func width(s string) int { return ansi.StringWidth(s) }

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-width(s))
}

func rjust(s string, w int) string {
	if width(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-width(s)) + s
}

// box draws a rounded panel with the title set into the top border.
func (f *Frame) box(title string, w, h int, lines []string, accent lipgloss.Style) []string {
	if w < 4 || h < 2 {
		return nil
	}
	g := f.G
	t := ""
	if title != "" {
		t = g.H + " " + accent.Bold(true).Render(title) + " "
	}
	top := sHair.Render(g.TL) + t + sHair.Render(strings.Repeat(g.H, max(0, w-2-width(t)))+g.TR)
	out := []string{top}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, sHair.Render(g.V)+fit(" "+l, w-2)+sHair.Render(g.V))
	}
	out = append(out, sHair.Render(g.BL+strings.Repeat(g.H, w-2)+g.BR))
	return out
}

func hjoin(a, b []string, aw int) []string {
	n := max(len(a), len(b))
	out := make([]string, n)
	for i := 0; i < n; i++ {
		l := ""
		if i < len(a) {
			l = a[i]
		}
		r := ""
		if i < len(b) {
			r = b[i]
		}
		out[i] = fit(l, aw) + r
	}
	return out
}

// bar is a smooth countdown bar (eighth-blocks).
func (f *Frame) bar(frac float64, w int, st lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(1, frac))
	cells := frac * float64(w)
	full := int(cells)
	part := int((cells - float64(full)) * 8)
	s := strings.Repeat(f.G.Bar[8], full)
	n := full
	if part > 0 && n < w {
		s += f.G.Bar[part]
		n++
	}
	return st.Render(s) + sHair.Render(strings.Repeat(f.G.Empty, w-n))
}

// spark renders vals right-aligned into w cells. Braille packs two samples
// per cell with four levels per row; rows stack for more resolution.
func (f *Frame) spark(vals []float64, w, rows int, st lipgloss.Style) []string {
	per := 1
	if f.G.Braille {
		per = 2
	}
	n := w * per
	if len(vals) > n {
		vals = vals[len(vals)-n:]
	}
	pad := make([]float64, n-len(vals))
	for i := range pad {
		pad[i] = -1
	}
	vals = append(pad, vals...)
	lo, hi := math.Inf(1), 0.0
	for _, v := range vals {
		if v >= 0 {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if math.IsInf(lo, 1) || hi-lo < 1 {
		lo = 0
	}
	lo = math.Max(0, lo-(hi-lo)*0.15) // keep the lowest sample visible
	hi = math.Max(hi, lo+1)
	out := make([]string, rows)
	levels := 4
	if !f.G.Braille {
		levels = 8
	}
	total := rows * levels
	for r := 0; r < rows; r++ {
		var b strings.Builder
		base := (rows - 1 - r) * levels // levels below this row
		for c := 0; c < w; c++ {
			if f.G.Braille {
				var bits rune
				for k := 0; k < 2; k++ {
					v := vals[c*2+k]
					if v < 0 {
						continue
					}
					hgt := int(math.Round((v-lo)/(hi-lo)*float64(total-1))) + 1
					fill := min(levels, max(0, hgt-base))
					col := [][4]rune{{0x40, 0x04, 0x02, 0x01}, {0x80, 0x20, 0x10, 0x08}}[k]
					for d := 0; d < fill; d++ {
						bits |= col[d]
					}
				}
				b.WriteRune(0x2800 + bits)
			} else {
				v := vals[c]
				if v < 0 {
					b.WriteString(" ")
					continue
				}
				hgt := int(math.Round((v-lo)/(hi-lo)*float64(total-1))) + 1
				fill := min(levels, max(0, hgt-base))
				b.WriteString(f.G.Spark[fill])
			}
		}
		out[r] = st.Render(b.String())
	}
	return out
}

func dur(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func commas(n uint64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// ---- panels ----

func (f *Frame) header(w int) []string {
	s, g := f.S, f.G
	flash := s.Flash.Active(f.Now)
	logo := sCobalt.Render(g.DevOn+g.Link) + sLilac.Render(" "+g.Agent)
	name := sBold.Render(" MESHY ") + sMuted.Render("mission control")
	where := sInk.Render(s.Org)
	if s.Me != "" {
		where += sMuted.Render("  "+g.Dot+"  ") + sCobalt.Render(short(s.Me, s.Org))
	}
	mode := ""
	switch s.Mode {
	case "demo":
		mode = " " + chipSun("DEMO")
	case "daemon":
		mode = " " + chipCyan("meshyd")
	case "agent":
		mode = " " + chipLilac("agent")
	}
	left := logo + name + "   " + where + mode

	// chain ticker: the number rolls in for 0.6 s when a block lands
	chain := sMuted.Render("sepolia ")
	if s.Head == 0 {
		spin := g.Spinner[f.Tick%len(g.Spinner)]
		chain += sMuted.Render(spin + " connecting")
		if s.HeadErr != "" {
			chain = sMuted.Render("sepolia ") + sPink.Render("unreachable")
		}
	} else {
		num := "#" + commas(s.Head)
		if since := f.Now.Sub(s.headBump); !s.headBump.IsZero() && since < 600*time.Millisecond {
			num = lipgloss.NewStyle().Background(cCobalt).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(num)
		} else {
			num = sBold.Render(num)
		}
		age := f.Now.Sub(s.HeadTime)
		ageSt := sLime
		dot := g.Bullet + " fresh"
		if age > 30*time.Second {
			ageSt, dot = sSun, g.Bullet+" slow"
		}
		if age > 45*time.Second {
			ageSt, dot = sPink, g.Bullet+" STALE"
		}
		chain += num + " " + ageSt.Render(dot) + sMuted.Render(" "+dur(age.Truncate(time.Second))+" ago")
		if d := s.Daemon; d != nil && d.HeadNumber > 0 && s.Head >= d.HeadNumber {
			lag := s.Head - d.HeadNumber
			st := sLime
			if lag > 1 {
				st = sSun
			}
			if !d.Healthy {
				st = sRed
			}
			chain += sMuted.Render("  daemon lag ") + st.Render(fmt.Sprintf("%d blk", lag))
		}
	}
	clock := sMuted.Render(f.Now.Format("15:04:05"))
	right := chain + "  " + clock
	gap := w - width(left) - width(right) - 2
	line := " " + left + strings.Repeat(" ", max(1, gap)) + right + " "
	if flash && (f.Tick/3)%2 == 0 && f.Now.Sub(s.Flash.At) < 3*time.Second {
		line = lipgloss.NewStyle().Background(cRed).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(fit(ansi.Strip(line), w))
	}
	return []string{fit(line, w)}
}

func (f *Frame) banner(w int) []string {
	s := f.S
	if !s.Flash.Active(f.Now) {
		return nil
	}
	age := f.Now.Sub(s.Flash.At)
	txt := " " + f.G.Cut + "  " + s.Flash.Text + "  " + f.G.Cut + " "
	st := lipgloss.NewStyle().Background(cRed).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	if age > 3*time.Second || (f.Tick/2)%2 == 0 {
		st = lipgloss.NewStyle().Background(cPink).Foreground(lipgloss.Color("#4A0D2B")).Bold(true)
	}
	if width(txt) > w {
		txt = ansi.Truncate(txt, w, "…")
	}
	padl := max(0, (w-width(txt))/2)
	return []string{strings.Repeat(" ", padl) + st.Render(txt)}
}

// topology draws org → networks → devices → services, with an edge per grant
// on the right: packets flow toward the device while a session is live.
func (f *Frame) topology(w int) []string {
	s, g := f.S, f.G
	type row struct{ tree, target string }
	var rows []row
	rows = append(rows, row{sTitle.Render(g.Org + " " + s.Org), ""})
	for ni, n := range s.Networks {
		lastN := ni == len(s.Networks)-1
		pre, cont := g.Tee, g.Pipe
		if lastN {
			pre, cont = g.Elbow, "  "
		}
		rows = append(rows, row{sHair.Render(pre) + sCyan.Render(g.Net+" "+n.Name), ""})
		for di, d := range n.Devices {
			lastD := di == len(n.Devices)-1
			dp, dc := g.Tee, g.Pipe
			if lastD {
				dp, dc = g.Elbow, "  "
			}
			live := len(f.S.sessionsFor(d.Full)) > 0 || f.S.recentActivity(d.Full, f.Now)
			glyph := sMuted.Render(g.DevOff)
			if live {
				glyph = sLime.Render(g.DevPulse[(f.Tick/3)%len(g.DevPulse)])
			} else if len(f.S.grantsFor(d.Full)) > 0 {
				glyph = sCobalt.Render(g.DevOn)
			}
			kind := ""
			if d.Kind != "" && w > 70 {
				kind = sMuted.Render(" " + d.Kind)
			}
			rows = append(rows, row{sHair.Render(cont+dp) + glyph + " " + sInk.Render(d.Name) + kind, d.Full})
			for si, sv := range d.Services {
				sp := g.Tee
				if si == len(d.Services)-1 {
					sp = g.Elbow
				}
				sg := sMuted.Render(g.Svc)
				if f.S.recentActivity(sv.Full, f.Now) {
					sg = sLime.Render(g.SvcOn)
				} else if len(f.S.grantsFor(sv.Full)) > 0 {
					sg = sCobalt.Render(g.Svc)
				}
				rows = append(rows, row{sHair.Render(cont+dc+sp) + sg + " " + sInk.Render(sv.Name), sv.Full})
			}
		}
	}
	treeW := 0
	for _, r := range rows {
		treeW = max(treeW, width(r.tree))
	}
	treeW += 1
	edgeW := max(6, min(18, w-treeW-16))
	var out []string
	for _, r := range rows {
		gs := []Grant(nil)
		if r.target != "" {
			gs = s.grantsFor(r.target)
		}
		if len(gs) == 0 {
			out = append(out, r.tree)
			continue
		}
		for i, gr := range gs {
			left := fit(r.tree, treeW)
			if i > 0 {
				left = strings.Repeat(" ", treeW)
			}
			out = append(out, left+f.edge(gr, edgeW))
		}
	}
	return out
}

func (f *Frame) edge(gr Grant, w int) string {
	g := f.G
	agent := label(gr.Agent)
	sessions := f.S.sessionsFor(gr.Target)
	active := false
	for _, se := range sessions {
		if se.Grant == gr.Name {
			active = true
		}
	}
	if !active && gr.Status == "live" && f.S.recentActivity(gr.Target, f.Now) {
		active = true
	}
	switch {
	case gr.Status == "revoked" || (f.S.Flash.Active(f.Now) && f.S.Flash.Grant == gr.Name):
		mid := w / 2
		line := strings.Repeat(g.LinkIdle, mid-1) + " " + g.Cut + " " + strings.Repeat(g.LinkIdle, max(0, w-mid-2))
		return sPink.Render(g.Arrow+line) + " " + sPink.Render(g.Agent+" "+agent) + " " + chipPink("revoked")
	case gr.Status != "live" || gr.Expiry.Before(f.Now):
		return sHair.Render(g.Arrow+strings.Repeat(g.LinkIdle, w)) + " " + sMuted.Render(g.Agent+" "+agent+" expired")
	case active:
		// two packets travelling right → left (agent → device)
		cells := make([]string, w)
		for i := range cells {
			cells[i] = sCobalt.Render(g.Link)
		}
		p := w - 1 - (f.Tick % w)
		cells[p] = sLime.Render(g.Packet)
		if q := (p + w/2) % w; q != p {
			cells[q] = sLime.Render(g.Packet)
		}
		return sLime.Render(g.Arrow) + strings.Join(cells, "") + " " + sLilac.Render(g.Agent+" "+agent)
	default:
		return sCobalt.Render(g.Arrow) + sHair.Render(strings.Repeat(g.LinkIdle, w)) + " " + sLilac.Render(g.Agent+" "+agent)
	}
}

func (f *Frame) grants(w int) []string {
	s := f.S
	var out []string
	if len(s.Grants) == 0 {
		if s.MapErr != "" {
			return []string{sPink.Render("map: " + s.MapErr)}
		}
		return []string{sMuted.Render("no grants")}
	}
	nameW := min(30, max(16, w-28))
	barW := max(4, w-nameW-16)
	const horizon = 15 * time.Minute
	for i, gr := range s.Grants {
		left := gr.Expiry.Sub(f.Now)
		nm := sLilac.Render(label(gr.Agent)) + sMuted.Render(" "+f.G.To+" ") + sInk.Render(short(gr.Target, s.Org))
		var st lipgloss.Style
		var right string
		switch {
		case gr.Status == "revoked":
			right = chipPink("REVOKED")
			out = append(out, f.selRow(1, i, fit(nm, nameW)+" "+sPink.Render(strings.Repeat(f.G.Cut, barW))+" "+right, w))
			continue
		case gr.Status != "live" || left <= 0:
			right = sMuted.Render("expired")
			out = append(out, f.selRow(1, i, fit(nm, nameW)+" "+sHair.Render(strings.Repeat(f.G.Empty, barW))+" "+right, w))
			continue
		case left < 2*time.Minute:
			st, right = sPink, sPink.Render(rjust(dur(left), 7))
		case left < 5*time.Minute:
			st, right = sSun, sSun.Render(rjust(dur(left), 7))
		default:
			st, right = sLime, sLime.Render(rjust(dur(left), 7))
		}
		frac := float64(left) / float64(horizon)
		out = append(out, f.selRow(1, i, fit(nm, nameW)+" "+f.bar(frac, barW, st)+" "+right, w))
	}
	return out
}

func (f *Frame) selRow(pane, i int, s string, w int) string {
	if f.Focus == pane && f.Sel == i {
		return sSel.Render(fit(ansi.Strip(s), w-1))
	}
	return s
}

func (f *Frame) sessions(w int) []string {
	s := f.S
	if s.Daemon == nil {
		if s.DaemonErr != "" {
			return []string{sMuted.Render(s.DaemonErr)}
		}
		return []string{sMuted.Render("(sessions are shown by `meshyd top` on the device)")}
	}
	var out []string
	if fl := s.Flash; fl.Active(f.Now) && fl.Session != "" {
		alive := false
		for _, se := range s.Daemon.Sessions {
			alive = alive || se.ID == fl.Session
		}
		if !alive {
			id := fl.Session
			if len(id) > 6 {
				id = id[:6]
			}
			kill := chipPink("KILLED")
			if (f.Tick/2)%2 == 0 && f.Now.Sub(fl.At) < 3*time.Second {
				kill = chip(cRed, "#FFFFFF", "KILLED")
			}
			out = append(out, sPink.Render(f.G.Cut+" "+id+" "+fit(fl.Agent, 9))+" "+kill+sPink.Render(fmt.Sprintf(" block #%d +%.0f ms", fl.Block, fl.MS)))
		}
	}
	if len(s.Daemon.Sessions) == 0 && len(out) == 0 {
		return []string{sMuted.Render("no live sessions")}
	}
	for i, se := range s.Daemon.Sessions {
		spin := sLime.Render(f.G.Spinner[(f.Tick+i*3)%len(f.G.Spinner)])
		left := se.Expiry.Sub(f.Now)
		total := se.Expiry.Sub(se.Started)
		frac := 1.0
		if total > 0 {
			frac = float64(left) / float64(total)
		}
		id := se.ID
		if len(id) > 6 {
			id = id[:6]
		}
		cmd := strings.Join(se.Argv, " ")
		head := spin + " " + sMuted.Render(id) + " " + sLilac.Render(fit(se.Agent, 9)) + " "
		barW := 8
		tail := " " + f.bar(frac, barW, sCobalt) + " " + sMuted.Render(rjust(dur(left), 6))
		mid := fit(sInk.Render("$ "+cmd), max(4, w-width(head)-width(tail)-1))
		out = append(out, f.selRow(0, i, head+mid+tail, w))
		if w > 50 {
			out = append(out, "    "+sMuted.Render(fit(se.Paths+"  "+f.G.Dot+" up "+dur(f.Now.Sub(se.Started)), w-5)))
		}
	}
	return out
}

func (f *Frame) chain(w, rows int) []string {
	s := f.S
	lw := 9
	sw := w - lw - 9
	var out []string
	txs := 0.0
	if n := len(s.TxBlocks); n > 0 {
		txs = s.TxBlocks[n-1]
	}
	for i, l := range f.spark(s.TxBlocks, sw, rows, sCobalt) {
		lab, val := "", ""
		if i == rows-1 {
			lab, val = "tx/block", fmt.Sprintf("%.0f", txs)
		}
		out = append(out, sMuted.Render(fit(lab, lw))+l+" "+sBold.Render(rjust(val, 6)))
	}
	if len(s.OpsPerSec) > 0 || s.Daemon != nil {
		ops := 0.0
		if n := len(s.OpsPerSec); n > 0 {
			ops = s.OpsPerSec[n-1]
		}
		for i, l := range f.spark(s.OpsPerSec, sw, rows, sLime) {
			lab, val := "", ""
			if i == rows-1 {
				lab, val = "ops/s", fmt.Sprintf("%.0f", ops)
			}
			out = append(out, sMuted.Render(fit(lab, lw))+l+" "+sBold.Render(rjust(val, 6)))
		}
	}
	return out
}

var bigDigits = map[rune][3]string{
	'0': {"┏━┓", "┃ ┃", "┗━┛"}, '1': {" ┓ ", " ┃ ", " ┻ "}, '2': {"┏━┓", "┏━┛", "┗━━"},
	'3': {"━━┓", " ━┫", "━━┛"}, '4': {"┃ ┃", "┗━┫", "  ┻"}, '5': {"┏━━", "┗━┓", "━━┛"},
	'6': {"┏━━", "┣━┓", "┗━┛"}, '7': {"━━┓", "  ┃", "  ┻"}, '8': {"┏━┓", "┣━┫", "┗━┛"},
	'9': {"┏━┓", "┗━┫", "━━┛"}, '-': {"   ", "━━━", "   "},
}

func (f *Frame) big(s string) []string {
	if !f.G.Braille {
		return []string{s}
	}
	rows := []string{"", "", ""}
	for _, c := range s {
		d, ok := bigDigits[c]
		if !ok {
			d = [3]string{" ", " ", " "}
		}
		for i := range rows {
			rows[i] += d[i] + " "
		}
	}
	return rows
}

// pulse: the revoke → kill latency in big digits, plus counters.
func (f *Frame) pulse(w int) []string {
	s := f.S
	var last *audit.Event
	blocked, denied, revokes := 0, 0, 0
	var sum float64
	for i := range s.Feed {
		e := &s.Feed[i]
		switch e.Kind {
		case audit.SessionRevoke:
			revokes++
			sum += e.Latency
			if last == nil {
				last = e
			}
		case audit.Blocked:
			blocked++
		case audit.Denied:
			denied++
		}
	}
	live := 0
	for _, g := range s.Grants {
		if g.Status == "live" && g.Expiry.After(f.Now) {
			live++
		}
	}
	nsess := 0
	if s.Daemon != nil {
		nsess = len(s.Daemon.Sessions)
	}
	var out []string
	if last != nil && last.Latency > 0 {
		st := sCobalt
		if s.Flash.Active(f.Now) {
			st = sRed
		}
		for i, l := range f.big(fmt.Sprintf("%.0f", last.Latency)) {
			tail := ""
			switch i {
			case 0:
				tail = sMuted.Render("  revoke → kill")
			case 1:
				tail = sBold.Render("  ms") + sMuted.Render(fmt.Sprintf(" after head #%d", last.Block))
			case 2:
				tail = sMuted.Render(fmt.Sprintf("  %s · avg %.0f ms over %d", last.Agent, sum/float64(revokes), revokes))
			}
			out = append(out, st.Render(l)+tail)
		}
	} else {
		out = append(out, sMuted.Render("no revokes seen yet: revoke a grant in ENS and"), sMuted.Render("the session dies within a block"))
	}
	out = append(out, "",
		sMuted.Render("sessions ")+sBold.Render(fmt.Sprint(nsess))+sMuted.Render("   grants live ")+sBold.Render(fmt.Sprint(live))+
			sMuted.Render("   blocked ")+sPink.Render(fmt.Sprint(blocked))+sMuted.Render("   denied ")+sPink.Render(fmt.Sprint(denied)))
	return out
}

func kindChip(k audit.Kind) string {
	switch k {
	case audit.SessionStart:
		return chipLime("START ")
	case audit.SessionEnd:
		return chipSun(" END  ")
	case audit.SessionRevoke:
		return chipPink("REVOKE")
	case audit.Blocked:
		return chipPink("BLOCK ")
	case audit.Denied:
		return chipPink(" DENY ")
	case audit.Op:
		return sMuted.Render("   op   ")
	}
	return sMuted.Render(fmt.Sprintf(" %-6s ", k))
}

func human(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fkB", float64(b)/(1<<10))
	case b > 0:
		return fmt.Sprintf("%dB", b)
	}
	return ""
}

func (f *Frame) feed(w, n int) []string {
	s := f.S
	if len(s.Feed) == 0 {
		if s.FeedErr != "" {
			return []string{sPink.Render("feed: " + s.FeedErr)}
		}
		return []string{sMuted.Render(f.G.Spinner[f.Tick%len(f.G.Spinner)] + " waiting for events…")}
	}
	var out []string
	feed := s.Feed
	for i := 0; i < len(feed); i++ {
		e := feed[i]
		if len(out) >= n {
			break
		}
		if e.Kind == audit.Op { // fold a burst of ops from one session into one row
			j, bytes, kinds := i, int64(0), map[string]int{}
			for j < len(feed) && feed[j].Kind == audit.Op && feed[j].Session == e.Session && e.TS.Sub(feed[j].TS) < 3*time.Second {
				bytes += feed[j].Bytes
				kinds[feed[j].Op]++
				j++
			}
			if j-i > 1 {
				w := ""
				if n := kinds["write"]; n == 1 {
					w = sSun.Render(" 1 write")
				} else if n > 1 {
					w = sSun.Render(fmt.Sprintf(" %d writes", n))
				}
				ts := sMuted.Render(e.TS.Format("15:04:05"))
				if f.Now.Sub(e.TS) < 1500*time.Millisecond {
					ts = sBold.Render(e.TS.Format("15:04:05"))
				}
				detail := sCobalt.Render(fmt.Sprintf("%s%-3d", f.G.Times, j-i)) + sMuted.Render(fmt.Sprintf(" %7s", human(bytes))) + w + sMuted.Render("  … ") + sInk.Render(e.Path)
				out = append(out, ts+" "+kindChip(e.Kind)+" "+sLilac.Render(fit(e.Agent, 9))+" "+detail)
				i = j - 1
				continue
			}
		}
		ts := sMuted.Render(e.TS.Format("15:04:05"))
		if f.Now.Sub(e.TS) < 1500*time.Millisecond {
			ts = sBold.Render(e.TS.Format("15:04:05"))
		}
		var detail string
		switch e.Kind {
		case audit.SessionRevoke:
			detail = sPink.Render(fmt.Sprintf("%s · block #%d · killed %.0f ms after head", short(e.Grant, s.Org), e.Block, e.Latency))
		case audit.Blocked:
			detail = sPink.Render(fmt.Sprintf("%s %s", e.Op, e.Path)) + sMuted.Render("  "+e.Reason)
		case audit.Denied:
			detail = sPink.Render(e.Reason)
		case audit.SessionStart, audit.SessionEnd:
			detail = sInk.Render(short(e.Grant, s.Org)) + sMuted.Render("  "+e.Session)
		case audit.Op:
			detail = sMuted.Render(fmt.Sprintf("%-8s", e.Op)) + sInk.Render(e.Path) + sMuted.Render(" "+human(e.Bytes))
		default:
			detail = sMuted.Render(e.Reason)
		}
		ag := sLilac.Render(fit(e.Agent, 9))
		out = append(out, ts+" "+kindChip(e.Kind)+" "+ag+" "+detail)
	}
	return out
}

func (f *Frame) footer(w int) string {
	keys := [][2]string{{"q", "quit"}, {"↑↓", "select"}, {"tab", "pane"}}
	if f.S.Daemon != nil && f.S.Mode != "agent" {
		keys = append(keys, [2]string{"k", "kill"})
	}
	keys = append(keys, [2]string{"c", "copy ssh"}, [2]string{"o", "dashboard"}, [2]string{"p", "pause"}, [2]string{"?", "help"})
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(sCobalt.Bold(true).Render(k[0]) + sMuted.Render(" "+k[1]+"  "))
	}
	l := " " + b.String()
	r := ""
	if f.Status != "" {
		r = sSun.Render(f.Status) + " "
	} else if f.Paused {
		r = chipSun("PAUSED") + " "
	}
	return fit(l+strings.Repeat(" ", max(1, w-width(l)-width(r)))+r, w)
}

func (f *Frame) helpLines() []string {
	return []string{
		sBold.Render("Keys"),
		"",
		"  ↑ ↓ / j       select a session or grant",
		"  tab           switch pane (sessions ⇄ grants)",
		"  k  then y     kill the selected session (meshyd only)",
		"  c             copy `meshy ssh <device> --` for the selected grant (OSC 52)",
		"  o             open this org on the web dashboard",
		"  p             pause the animation (data keeps loading)",
		"  a             toggle ASCII glyphs",
		"  q / ctrl-c    quit",
		"",
		sBold.Render("Legend"),
		"",
		"  " + f.G.Org + " org   " + f.G.Net + " network   " + f.G.DevOn + " device   " + f.G.Svc + " service   " + f.G.Agent + " agent",
		"  " + f.G.Link + f.G.Packet + f.G.Link + "  live session (packets flow agent → device)",
		"  " + f.G.LinkIdle + f.G.LinkIdle + f.G.LinkIdle + "  grant, idle      " + f.G.Cut + "  revoked",
		"",
		sMuted.Render("Agents and scripts: `--json` prints the same state as JSON."),
	}
}

// Render draws the full screen.
func Render(f *Frame) string {
	w, h := f.W, f.H
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	if f.Splash > 0 {
		return splashFrame(f, w, h)
	}
	if w < 60 || h < 16 {
		return compact(f, w, h)
	}
	var out []string
	out = append(out, f.header(w)...)
	out = append(out, f.banner(w)...)
	if f.Help {
		out = append(out, f.box("HELP", w, h-len(out)-1, f.helpLines(), sCobalt)...)
		out = append(out, f.footer(w))
		return strings.Join(out, "\n")
	}
	body := h - len(out) - 1
	wide := w >= 110
	if wide {
		lw := w * 52 / 100
		rw := w - lw
		topo := f.topology(lw - 3)
		ses := f.sessions(rw - 3)
		gra := f.grants(rw - 3)
		topH := max(len(topo)+2, len(ses)+len(gra)+5)
		topH = min(topH, body*55/100)
		sesH := min(len(ses)+2, topH/2)
		left := f.box("TOPOLOGY", lw, topH, topo, sCobalt)
		right := append(f.box("SESSIONS", rw, sesH, ses, sLime), f.box("GRANTS", rw, topH-sesH, gra, sLilac)...)
		out = append(out, hjoin(left, right, lw)...)
		rest := body - topH
		chainRows := 2
		chainLines := f.chain(rw-3, chainRows)
		feedLines := f.feed(lw-3, rest-2)
		chainH := min(rest, len(chainLines)+2)
		rightCol := f.box("CHAIN · SEPOLIA", rw, chainH, chainLines, sCobalt)
		if rest-chainH >= 5 {
			rightCol = append(rightCol, f.box("REVOKE → KILL", rw, rest-chainH, f.pulse(rw-3), sPink)...)
		}
		out = append(out, hjoin(f.box("AUDIT FEED", lw, rest, feedLines, sPink), rightCol, lw)...)
	} else {
		topo := f.topology(w - 3)
		ses := f.sessions(w - 3)
		gra := f.grants(w - 3)
		chainLines := f.chain(w-3, 1)
		sesH := min(len(ses)+2, 6)
		graH := min(len(gra)+2, 7)
		chH := len(chainLines) + 2
		topH := max(3, min(len(topo)+2, body-sesH-graH-chH-6))
		parts := []struct {
			t  string
			l  []string
			st lipgloss.Style
			h  int
		}{{"TOPOLOGY", topo, sCobalt, topH}, {"SESSIONS", ses, sLime, sesH}, {"GRANTS", gra, sLilac, graH}, {"CHAIN · SEPOLIA", chainLines, sCobalt, chH}}
		for _, p := range parts {
			if body-p.h < 4 {
				break
			}
			out = append(out, f.box(p.t, w, p.h, p.l, p.st)...)
			body -= p.h
		}
		if body >= 3 {
			out = append(out, f.box("AUDIT FEED", w, body, f.feed(w-3, body-2), sPink)...)
		}
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	out = append(out[:h-1], f.footer(w))
	return strings.Join(out, "\n")
}

// compact is for tiny terminals: one line per fact.
func compact(f *Frame, w, h int) string {
	s := f.S
	var out []string
	out = append(out, fit(sBold.Render("MESHY ")+sMuted.Render(s.Org)+"  #"+commas(s.Head), w))
	out = append(out, f.banner(w)...)
	if s.Daemon != nil {
		out = append(out, fit(fmt.Sprintf("%d sessions", len(s.Daemon.Sessions)), w))
	}
	for _, l := range f.grants(w) {
		out = append(out, fit(l, w))
	}
	for _, l := range f.feed(w, h) {
		out = append(out, fit(l, w))
	}
	if len(out) > h {
		out = out[:h]
	}
	return strings.Join(out, "\n")
}
