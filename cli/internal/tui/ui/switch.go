package ui

import (
	"image/color"
	"math"
)

// a pill switch, off at 0 and on at 1; the knob slides between
func Switch(frac float64, bg color.Color) string {
	p := P()
	if G.ASCII {
		if frac >= .5 {
			return "[on ]"
		}
		return "[off]"
	}
	track := Mix(Mix(p.Bg, p.Text, .26), p.Accent, frac)
	knob := p.Text
	if !p.Dark {
		knob = p.Bg
	}
	knob = Mix(Mix(track, knob, .7), knob, frac)
	const inner = 4
	at := int(math.Round(frac * (inner - 1)))
	l := L(bg).T("▐", track)
	for i := range inner {
		c := " "
		if i == at {
			c = "●"
		}
		l.Raw(Paint(c, knob, track, true))
	}
	return l.T("▌", track).String()
}
