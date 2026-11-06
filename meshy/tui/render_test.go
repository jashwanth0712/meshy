package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Every frame of the demo, at every size and in both glyph sets, must fit
// the terminal exactly: no line wider than W, no more than H lines.
func TestDemoFramesFit(t *testing.T) {
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	sizes := [][2]int{{160, 48}, {140, 40}, {110, 32}, {100, 30}, {80, 24}, {60, 16}, {50, 14}, {30, 8}}
	for _, g := range []Glyphs{Unicode, ASCII} {
		for at := time.Duration(0); at < demoLoop; at += 700 * time.Millisecond {
			for _, sz := range sizes {
				for _, help := range []bool{false, true} {
					f := &Frame{S: demoState(at, now), Now: now, W: sz[0], H: sz[1], G: g, Tick: int(at / 100 / time.Millisecond), Help: help}
					out := Render(f)
					lines := strings.Split(out, "\n")
					if len(lines) > sz[1] {
						t.Fatalf("at %v %dx%d: %d lines", at, sz[0], sz[1], len(lines))
					}
					for i, l := range lines {
						if w := width(l); w > sz[0] {
							t.Fatalf("at %v %dx%d line %d is %d wide: %q", at, sz[0], sz[1], i, w, l)
						}
					}
				}
			}
		}
	}
}

func TestDemoRevokeStory(t *testing.T) {
	now := time.Now()
	before := demoState(19*time.Second, now)
	after := demoState(21*time.Second, now)
	if before.Flash != nil || before.Grants[0].Status != "live" {
		t.Fatal("no revoke expected before 20s")
	}
	if !after.Flash.Active(now) || after.Grants[0].Status != "revoked" || after.Flash.MS != 1316 {
		t.Fatalf("revoke expected at 21s: %+v", after.Flash)
	}
	for _, s := range after.Daemon.Sessions {
		if s.ID == after.Flash.Session {
			t.Fatal("revoked session still listed")
		}
	}
	out := Render(&Frame{S: after, Now: now, W: 140, H: 40, G: Unicode})
	for _, want := range []string{"REVOKED", "KILLED", "1316 ms", "REVOKE → KILL"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
}

func TestSplashFits(t *testing.T) {
	s := demoState(0, time.Now())
	for _, p := range []float64{0.01, 0.3, 0.6, 0.9, 1} {
		out := Render(&Frame{S: s, Now: time.Now(), W: 90, H: 28, G: Unicode, Splash: p})
		if n := len(strings.Split(out, "\n")); n > 28 {
			t.Fatalf("splash %v: %d lines", p, n)
		}
	}
	if !strings.Contains(Logo(false), "✦") || strings.Contains(Logo(true), "✦") {
		t.Fatal("logo glyph sets")
	}
}

func TestSparkLevels(t *testing.T) {
	f := &Frame{G: Unicode}
	rows := f.spark([]float64{0, 1, 2, 3, 4, 5, 6, 7}, 4, 2, sBold)
	if len(rows) != 2 || width(rows[0]) != 4 {
		t.Fatalf("rows %q", rows)
	}
	f.G = ASCII
	if r := f.spark(nil, 5, 1, sBold); width(r[0]) != 5 {
		t.Fatalf("empty spark %q", r)
	}
}

// --json is the agent contract: valid JSON with the state's fields.
func TestJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := Run(context.Background(), Options{Demo: true, JSON: true, At: 21 * time.Second, Out: &buf}); err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(buf.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"org", "networks", "grants", "head", "daemon", "feed", "flash"} {
		if _, ok := st[k]; !ok {
			t.Errorf("json lacks %q", k)
		}
	}
}
