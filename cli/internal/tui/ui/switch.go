package ui

import (
	"image/color"
	"math"
	"os"
	"strings"
)

// terminals that draw powerline's rounded caps themselves, so no special font is needed
var roundCaps = func() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "vscode", "ghostty", "WezTerm":
		return true
	}
	return os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(os.Getenv("TERM"), "kitty")
}()

// a switch, off at 0 and on at 1, its knob sliding between: a rounded pill, or a track and dot where caps can't be drawn
func Switch(frac float64, bg color.Color) string {
	p := P()
	if G.ASCII {
		if frac >= .5 {
			return "[on ]"
		}
		return "[off]"
	}
	off := Mix(p.Bg, p.Text, .22)
	if !roundCaps {
		track, knob := Mix(off, Mix(p.Bg, p.Accent, .5), frac), Mix(Mix(p.Bg, p.Text, .62), p.Accent, frac)
		at := int(math.Round(frac * 2))
		l := L(bg)
		for i := range 3 {
			if i == at {
				l.T("●", knob)
			} else {
				l.T(G.Heavy, track)
			}
		}
		return l.String()
	}
	on := p.Text
	if !p.Dark {
		on = p.Bg
	}
	track, knob := Mix(off, p.Accent, frac), Mix(Mix(p.Bg, p.Text, .62), on, frac)
	const n, left, right = 4, "\ue0b6", "\ue0b4"
	k := int(math.Round(frac * (n - 2)))
	var b strings.Builder
	for i := range n {
		under := track
		if i == 0 || i == n-1 {
			under = bg
		}
		switch i {
		case k:
			b.WriteString(Paint(left, knob, under, false))
		case k + 1:
			b.WriteString(Paint(right, knob, under, false))
		case 0:
			b.WriteString(Paint(left, track, bg, false))
		case n - 1:
			b.WriteString(Paint(right, track, bg, false))
		default:
			b.WriteString(Paint(" ", nil, track, false))
		}
	}
	return b.String()
}

// a small rounded count; plain where caps can't be drawn
func Badge(text string, under color.Color) string {
	p := P()
	if !roundCaps || G.ASCII {
		return Paint(text, p.Muted, under, false)
	}
	fill := Mix(p.Bg, p.Accent, .2)
	return Paint("", fill, under, false) + Paint(text, p.Accent, fill, true) + Paint("", fill, under, false)
}
