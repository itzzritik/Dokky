package water

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

func TestRenderWidth(t *testing.T) {
	for _, tc := range []struct {
		w, rows int
		energy  float64
		still   bool
	}{
		{1, 1, 1, false},
		{2, 3, 1, false},
		{40, 6, .3, false},
		{120, 14, 1, false},
		{200, 14, 2.5, false},
		{333, 5, 1, true},
		{80, 40, 1.4, false},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.w, tc.rows), func(t *testing.T) {
			wt := NewSeeded(tc.w, tc.rows, 3)
			wt.SetEnergy(tc.energy)
			wt.SetStill(tc.still)
			for f := range 40 {
				wt.Step()
				lines := Render(wt)
				if len(lines) != tc.rows {
					t.Fatalf("frame %d: %d lines, want %d", f, len(lines), tc.rows)
				}
				for i, l := range lines {
					if got := ansi.StringWidth(l); got != tc.w {
						t.Fatalf("frame %d row %d: width %d, want %d", f, i, got, tc.w)
					}
				}
				for _, d := range wt.Spray(10) {
					if d.X < 0 || d.X >= tc.w || d.Row < 0 || d.Row >= 10+tc.rows || d.Pos < 0 || d.Pos >= 1 {
						t.Fatalf("frame %d: drop out of range %+v", f, d)
					}
				}
			}
		})
	}
}

func TestNil(t *testing.T) {
	var wt *Water
	wt.Step()
	wt.SetEnergy(2)
	wt.SetStill(true)
	wt.SetPalette(fallback)
	if w, h := wt.Size(); w != 0 || h != 0 {
		t.Fatalf("size of nil = %d,%d", w, h)
	}
	if Render(wt) != nil || wt.Spray(5) != nil {
		t.Fatal("nil water drew something")
	}
	if New(0, 5) != nil || New(5, 0) != nil || New(-1, -1) != nil {
		t.Fatal("empty band should be nil")
	}
}

func TestSeeded(t *testing.T) {
	run := func(seed uint64) ([]string, []Drop) {
		wt := NewSeeded(90, 8, seed)
		wt.SetEnergy(1.6)
		for range 50 {
			wt.Step()
		}
		return Render(wt), slices.Clone(wt.Spray(6))
	}
	a, da := run(7)
	b, db := run(7)
	if !slices.Equal(a, b) || !slices.Equal(da, db) {
		t.Fatal("same seed gave different frames")
	}
	if c, _ := run(8); slices.Equal(a, c) {
		t.Fatal("different seeds gave the same frame")
	}
}

func TestStill(t *testing.T) {
	wt := NewSeeded(60, 6, 1)
	wt.SetStill(true)
	a := Render(wt)
	for range 10 {
		wt.Step()
	}
	if !slices.Equal(a, Render(wt)) {
		t.Fatal("still water moved")
	}
	if wt.Spray(4) != nil {
		t.Fatal("still water threw spray")
	}
}

func BenchmarkFrame(b *testing.B) {
	wt := NewSeeded(200, 14, 1)
	b.ReportAllocs()
	for b.Loop() {
		wt.Step()
		Render(wt)
		wt.Spray(10)
	}
}

type hole struct {
	x, y, w, h int
	box        bool
}

func (r hole) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// scene lays the band at row top of a w x h screen with spray around the holes, as the gate does
func scene(wt *Water, w, h, top int, holes ...hole) string {
	layers := []*lipgloss.Layer{lipgloss.NewLayer(strings.Join(Render(wt), "\n")).Y(top)}
	p := ui.P()
	for _, r := range holes {
		if !r.box {
			continue
		}
		box := []string{"╭" + strings.Repeat("─", r.w-2) + "╮"}
		for range r.h - 2 {
			box = append(box, "│"+strings.Repeat(" ", r.w-2)+"│")
		}
		box = append(box, "╰"+strings.Repeat("─", r.w-2)+"╯")
		layers = append(layers, lipgloss.NewLayer(ui.Paint(strings.Join(box, "\n"), p.Faint, nil, false)).X(r.x).Y(r.y))
	}
	for _, d := range wt.Spray(top) {
		if d.Row >= h || slices.ContainsFunc(holes, func(r hole) bool { return r.has(d.X, d.Row) }) {
			continue
		}
		layers = append(layers, lipgloss.NewLayer(ui.Paint(d.Glyph(), d.Fg, nil, false)).X(d.X).Y(d.Row))
	}
	c := lipgloss.NewCanvas(w, h)
	c.Compose(lipgloss.NewCompositor(layers...))
	return c.Render()
}

// WATER_SHOTS=dir writes frames and a gate-like preview as .ansi files to look at
func TestShots(t *testing.T) {
	dir := os.Getenv("WATER_SHOTS")
	if dir == "" {
		t.Skip("set WATER_SHOTS to a folder to write frames")
	}
	for _, theme := range []struct {
		name string
		bg   color.Color
	}{{"dark", lipgloss.Color("#000000")}, {"light", lipgloss.Color("#FFFFFF")}} {
		ui.SetTerminal(theme.bg)
		pal := Theme(ui.P())
		for _, w := range []int{120, 200} {
			wt := NewSeeded(w, 14, 1)
			wt.SetPalette(pal)
			for f := range 6 {
				for range 4 {
					wt.Step()
				}
				write(t, dir, fmt.Sprintf("%s-%d-%d.ansi", theme.name, w, f), scene(wt, w, 18, 4))
			}
		}
		for _, e := range []struct {
			name   string
			energy float64
			still  bool
		}{{"calm", .4, false}, {"lively", 1.8, false}, {"still", 1, true}} {
			wt := NewSeeded(120, 14, 2)
			wt.SetPalette(pal)
			wt.SetEnergy(e.energy)
			wt.SetStill(e.still)
			for range 60 {
				wt.Step()
			}
			write(t, dir, fmt.Sprintf("%s-%s.ansi", theme.name, e.name), scene(wt, 120, 18, 4))
		}
		wt := NewSeeded(100, 10, 5)
		wt.SetPalette(pal)
		for range 30 {
			wt.Step()
		}
		write(t, dir, fmt.Sprintf("preview-%s.ansi", theme.name), scene(wt, 100, 30, 20, hole{27, 3, 46, 5, false}, hole{24, 10, 52, 9, true}))
	}
	ui.SetTerminal(nil)
}

func write(t *testing.T, dir, name, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
