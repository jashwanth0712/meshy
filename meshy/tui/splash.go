package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The logo is an M drawn as a network (brand/README.md): solid node bottom
// left (your device), rings for linked devices, and the agent's spark top
// right, held apart from the mesh by a gap. Never close that gap.

type cell struct {
	r, c  int
	u, a  string // unicode, ascii
	style lipgloss.Style
}

const logoW, logoH = 19, 9

func logoCells() []cell {
	var cs []cell
	add := func(r, c int, u, a string, st lipgloss.Style) { cs = append(cs, cell{r, c, u, a, st}) }
	// drawing order = animation order
	add(8, 1, "●", "@", sCobalt)
	for r := 7; r >= 1; r-- {
		add(r, 1, "│", "|", sCobalt)
	}
	add(0, 1, "◯", "O", sCobalt)
	for i, c := range []int{3, 5, 7} {
		add(1+i, c, "╲", "\\", sCobalt)
	}
	add(4, 9, "◯", "O", sCobalt)
	for i, c := range []int{11, 13} { // stops short of the spark: the gap
		add(3-i, c, "╱", "/", sCobalt)
	}
	add(0, 17, "✦", "*", sLilac)
	for r := 2; r <= 7; r++ { // gap below the spark too
		add(r, 17, "│", "|", sCobalt)
	}
	add(8, 17, "◯", "O", sCobalt)
	return cs
}

var wordmark = []string{
	"█▀▄▀█ █▀▀ █▀ █ █ █▄█",
	"█ ▀ █ ██▄ ▄█ █▀█  █ ",
}

var wordmarkASCII = []string{
	"|\\/| |== /= |_| \\_/",
	"|  | |__ =/ | |  | ",
}

// logoLines renders the mark with the first n cells drawn (n<0: all).
func logoLines(g Glyphs, n int, sparkle int) []string {
	grid := make([][]string, logoH)
	for r := range grid {
		grid[r] = make([]string, logoW)
		for c := range grid[r] {
			grid[r][c] = " "
		}
	}
	cs := logoCells()
	if n < 0 || n > len(cs) {
		n = len(cs)
	}
	for i, c := range cs[:n] {
		ch := c.u
		if !g.Braille {
			ch = c.a
		}
		if ch == "✦" && sparkle > 0 {
			ch = []string{"✦", "✧", "✶", "✧"}[sparkle%4]
		}
		st := c.style
		if i == n-1 && n < len(cs) {
			st = sLime // the drawing "pen"
		}
		grid[c.r][c.c] = st.Render(ch)
	}
	out := make([]string, logoH)
	for r := range grid {
		out[r] = strings.Join(grid[r], "")
	}
	return out
}

// Logo is the static mark + wordmark, for `meshyd init` and friends.
func Logo(ascii bool) string {
	g := Unicode
	wm := wordmark
	if ascii {
		g, wm = ASCII, wordmarkASCII
	}
	lines := logoLines(g, -1, 0)
	lines = append(lines, "")
	for _, l := range wm {
		lines = append(lines, sBold.Render(l))
	}
	return strings.Join(lines, "\n")
}

func splashFrame(f *Frame, w, h int) string {
	cs := logoCells()
	n := int(f.Splash * 1.4 * float64(len(cs)))
	lines := logoLines(f.G, n, f.Tick/2)
	wm := wordmark
	if !f.G.Braille {
		wm = wordmarkASCII
	}
	lines = append(lines, "")
	if f.Splash > 0.55 {
		for _, l := range wm {
			lines = append(lines, sBold.Render(l))
		}
		lines = append(lines, "", sMuted.Render("scoped, revocable access for agents · ENSv2 on Sepolia"))
	}
	if f.Splash > 0.75 {
		lines = append(lines, "", sMuted.Render(f.G.Spinner[f.Tick%len(f.G.Spinner)]+" linking to "+f.S.Org+" …"))
	}
	block := lipgloss.JoinVertical(lipgloss.Center, lines...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
}
