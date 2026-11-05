package ui

import (
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
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

// whether the last two minutes show anything, so the graph keeps gliding
func Showing(h []api.Sample) bool {
	for _, s := range h {
		if s.Down+s.Up > 0 {
			return !Reduced()
		}
	}
	return false
}

// how far the newest sample has slid left since it arrived, 0..1
func Phase(h []api.Sample, now time.Time) float64 {
	if len(h) == 0 || Reduced() {
		return 0
	}
	return math.Max(0, math.Min(1, now.Sub(h[len(h)-1].At).Seconds()))
}

// a light rolling average, so 1 s bursts read as waves instead of spikes
func smooth(s []float64) []float64 {
	k := [...]float64{1, 2, 3, 2, 1}
	out := make([]float64, len(s))
	for i := range s {
		var sum, weight float64
		for d, kw := range k {
			if j := i + d - 2; j >= 0 && j < len(s) {
				sum, weight = sum+s[j]*kw, weight+kw
			}
		}
		out[i] = sum / weight
	}
	return out
}

// braille area chart of the last two minutes at a fixed scale, newest on the right, brighter towards the top, a one-dot water line at rest
func Graph(w, rows int, samples []float64, phase float64) []string {
	out := make([]string, rows)
	if w <= 0 || rows <= 0 {
		return out
	}
	// a short history starts on the right, so the graph always scrolls at one speed
	padded := make([]float64, max(0, api.HistoryLen-len(samples)), api.HistoryLen)
	s := smooth(append(padded, samples[max(0, len(samples)-api.HistoryLen):]...))
	top := 0.0
	for _, v := range s {
		top = math.Max(top, v)
	}
	top *= 1.15
	cols, n := 2*w, len(s)
	height := func(j int) float64 {
		if n < 3 || top <= 0 {
			return 1
		}
		u := math.Min(float64(n-1), phase+float64(j)*float64(n-2)/float64(max(1, cols-1)))
		i := int(u)
		v := s[i]
		if i+1 < n {
			v += (s[i+1] - s[i]) * (u - float64(i))
		}
		return math.Max(1, v/top*float64(rows*4))
	}
	for r := 0; r < rows; r++ {
		var b strings.Builder
		c := RampAt(.3 + .6*(1-float64(r)/math.Max(1, float64(rows-1))))
		for i := 0; i < w; i++ {
			bits := 0
			for dx := 0; dx < 2; dx++ {
				h := height(2*i + dx)
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
