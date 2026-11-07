package tui

import "github.com/charmbracelet/lipgloss"

// Palette follows docs/ui-revamp/PLAN.md §2 ("Electric"). Cobalt itself is
// unreadable on dark terminals (2.5:1), so dark backgrounds use cobalt 300.
var (
	cCobalt = lipgloss.AdaptiveColor{Light: "#1E3FD8", Dark: "#8C9BFF"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#5B6787", Dark: "#93A0C8"}
	cHair   = lipgloss.AdaptiveColor{Light: "#CDD5EC", Dark: "#2A3566"}
	cInk    = lipgloss.AdaptiveColor{Light: "#0A0F2C", Dark: "#F6F8FF"}
	cLime   = lipgloss.Color("#BEF264")
	cSun    = lipgloss.Color("#FDE047")
	cPink   = lipgloss.Color("#F9A8D4")
	cLilac  = lipgloss.Color("#C4B5FD")
	cCyan   = lipgloss.Color("#38BDF8")
	cNight  = lipgloss.Color("#050B24")
	cRed    = lipgloss.Color("#FF4D6D")
)

var (
	sBold   = lipgloss.NewStyle().Bold(true)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cCobalt)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sHair   = lipgloss.NewStyle().Foreground(cHair)
	sInk    = lipgloss.NewStyle().Foreground(cInk)
	sCobalt = lipgloss.NewStyle().Foreground(cCobalt)
	sLime   = lipgloss.NewStyle().Foreground(cLime)
	sSun    = lipgloss.NewStyle().Foreground(cSun)
	sPink   = lipgloss.NewStyle().Foreground(cPink)
	sLilac  = lipgloss.NewStyle().Foreground(cLilac)
	sCyan   = lipgloss.NewStyle().Foreground(cCyan)
	sRed    = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	sSel    = lipgloss.NewStyle().Reverse(true)
)

// chip renders a solid, saturated label (the dashboard's chips).
func chip(c lipgloss.Color, ink string, text string) string {
	return lipgloss.NewStyle().Background(c).Foreground(lipgloss.Color(ink)).Bold(true).Render(" " + text + " ")
}

func chipLime(t string) string  { return chip(cLime, "#1A2E05", t) }
func chipSun(t string) string   { return chip(cSun, "#3A2E00", t) }
func chipPink(t string) string  { return chip(cPink, "#4A0D2B", t) }
func chipLilac(t string) string { return chip(cLilac, "#2E1065", t) }
func chipCyan(t string) string  { return chip(cCyan, "#05213A", t) }

// Glyphs is the character set. Unicode by default; ASCII for TERM=dumb,
// --ascii, or fonts without box drawing / braille.
type Glyphs struct {
	TL, TR, BL, BR, H, V    string
	Tee, Elbow, Pipe        string
	Org, Net, DevOn, DevOff string
	DevPulse                []string
	Svc, SvcOn, Agent       string
	Link, LinkIdle, Packet  string
	Cut, Arrow, Dot         string
	To, Bullet, Times       string
	Bar                     []string // partial fill, empty → full
	Empty                   string
	Spark                   []string // block sparkline fallback
	Braille                 bool
	Spinner                 []string
}

var Unicode = Glyphs{
	TL: "╭", TR: "╮", BL: "╰", BR: "╯", H: "─", V: "│",
	Tee: "├─", Elbow: "└─", Pipe: "│ ",
	Org: "◆", Net: "▣", DevOn: "◉", DevOff: "○", DevPulse: []string{"◉", "◎", "◉", "●"},
	Svc: "◇", SvcOn: "◈", Agent: "✦",
	Link: "━", LinkIdle: "┄", Packet: "●", Cut: "╳", Arrow: "◂", Dot: "·",
	To: "→", Bullet: "●", Times: "×",
	Bar:     []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"},
	Empty:   "░",
	Spark:   []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"},
	Braille: true,
	Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
}

var ASCII = Glyphs{
	TL: "+", TR: "+", BL: "+", BR: "+", H: "-", V: "|",
	Tee: "|-", Elbow: "`-", Pipe: "| ",
	Org: "#", Net: "%", DevOn: "@", DevOff: "o", DevPulse: []string{"@", "O", "@", "*"},
	Svc: "<>", SvcOn: "<*>", Agent: "*",
	Link: "=", LinkIdle: ".", Packet: "o", Cut: "X", Arrow: "<", Dot: ".",
	To: "->", Bullet: "*", Times: "x",
	Bar:     []string{"", "#", "#", "#", "#", "#", "#", "#", "#"},
	Empty:   ".",
	Spark:   []string{" ", ".", ".", ":", ":", "=", "=", "#", "#"},
	Spinner: []string{"|", "/", "-", "\\"},
}
