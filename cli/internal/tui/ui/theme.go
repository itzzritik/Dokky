package ui

import (
	"image/color"
	"math"
	"sync"

	"charm.land/lipgloss/v2"
)

type Palette struct {
	Bg, Text, Muted, Faint, Line, Cap, Surface, Sel, Field, Window color.Color
	Accent, OnAccent, Kelp, Buoy, Coral                            color.Color
	Ramp                                                           [6]color.Color
	Dark                                                           bool
}

type base struct {
	bg, text, accent, onAccent, kelp, buoy, coral string
	ramp                                          [6]string
}

var (
	darkBase  = base{"#000000", "#ECEFF0", "#22B8CC", "#03161C", "#6ED69A", "#F2B544", "#F2665A", [6]string{"#062635", "#0A4E66", "#0E7F99", "#22B8CC", "#74E8E1", "#DDFFF9"}}
	lightBase = base{"#FFFFFF", "#16191B", "#0A8296", "#FFFFFF", "#15803D", "#A16207", "#B91C1C", [6]string{"#E4F6F8", "#B5E8EE", "#6FCFDD", "#1E9FB5", "#0B7086", "#06475A"}}

	mu     sync.RWMutex
	termBg color.Color
	pal    = build(darkBase, nil)
)

func hex(s string) color.Color { return lipgloss.Color(s) }

func build(b base, bg color.Color) *Palette {
	p := &Palette{Text: hex(b.text), Accent: hex(b.accent), OnAccent: hex(b.onAccent), Kelp: hex(b.kelp), Buoy: hex(b.buoy), Coral: hex(b.coral), Dark: b == darkBase}
	for i, c := range b.ramp {
		p.Ramp[i] = hex(c)
	}
	p.Bg = bg
	if bg == nil {
		p.Bg = hex(b.bg)
	}
	m := func(t float64) color.Color { return Mix(p.Bg, p.Text, t) }
	p.Muted, p.Faint, p.Line, p.Cap, p.Surface = m(.58), m(.36), m(.20), m(.10), m(.055)
	p.Sel = Mix(p.Bg, p.Accent, .16)
	// a window is the screen itself on light, a shade raised on dark; a field is a tint of the window
	p.Window = p.Bg
	if p.Dark {
		p.Window = m(.045)
	}
	p.Field = Mix(p.Window, p.Text, .055)
	return p
}

func dark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return .2126*float64(r>>8)+.7152*float64(g>>8)+.0722*float64(b>>8) < 128
}

func rebuild() {
	b := lightBase
	if termBg == nil || dark(termBg) {
		b = darkBase
	}
	pal = build(b, termBg)
}

func SetTerminal(bg color.Color) {
	mu.Lock()
	defer mu.Unlock()
	termBg = bg
	rebuild()
}

func P() *Palette {
	mu.RLock()
	defer mu.RUnlock()
	return pal
}

func Mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	f := func(x, y uint32) uint8 { return uint8(float64(x>>8)*(1-t) + float64(y>>8)*t + .5) }
	return color.RGBA{f(ar, br), f(ag, bg), f(ab, bb), 255}
}

func RampAt(f float64) color.Color {
	r := P().Ramp
	f = math.Max(0, math.Min(1, f)) * float64(len(r)-1)
	i := min(len(r)-2, int(f))
	return Mix(r[i], r[i+1], f-float64(i))
}
