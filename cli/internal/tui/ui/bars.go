package ui

import (
	"image/color"
	"math"
	"strings"
)

func ThinBar(w int, frac, t float64, tone color.Color) string {
	p := P()
	n := int(math.Round(float64(w) * math.Max(0, math.Min(1, frac))))
	var b strings.Builder
	for i := 0; i < w; i++ {
		c := p.Line
		if i < n {
			c = tone
			if c == nil {
				c = Mix(p.Accent, p.Ramp[4], Wave(i, .45, 6, t))
			}
		}
		b.WriteString(Paint(G.Heavy, c, nil, false))
	}
	return b.String()
}

func ThickBar(w int, frac, t float64, tone color.Color) string {
	p := P()
	n := int(math.Round(float64(w) * math.Max(0, math.Min(1, frac))))
	var b strings.Builder
	for i := 0; i < w; i++ {
		c := p.Cap
		if i < n {
			c = tone
			if c == nil {
				c = Mix(p.Ramp[2], p.Accent, Wave(i, .35, 3, t))
			}
		}
		b.WriteString(Paint(" ", nil, c, false))
	}
	return b.String()
}

var dots = [2][4]int{{1, 2, 4, 64}, {8, 16, 32, 128}}

// braille area chart, newest on the right, brighter towards the top
func Graph(w, rows int, samples []float64, top float64) []string {
	out := make([]string, rows)
	if w <= 0 || rows <= 0 {
		return out
	}
	if top <= 0 {
		top = 1
	}
	cols := 2 * w
	height := func(j int) float64 {
		k := len(samples) - cols + j
		if k < 0 || k >= len(samples) {
			return 0
		}
		return samples[k] / top * float64(rows*4)
	}
	for r := 0; r < rows; r++ {
		var b strings.Builder
		c := RampAt(.3 + .6*(1-float64(r)/math.Max(1, float64(rows-1))))
		for i := 0; i < w; i++ {
			bits := 0
			for dx := 0; dx < 2; dx++ {
				h := height(2*i + dx)
				if h > 0 {
					h = math.Max(1, h)
				}
				for dy := 0; dy < 4; dy++ {
					if float64((rows-1-r)*4+(3-dy)) < h {
						bits |= dots[dx][dy]
					}
				}
			}
			if bits == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(Paint(string(rune(0x2800+bits)), c, nil, false))
		}
		out[r] = b.String()
	}
	return out
}
