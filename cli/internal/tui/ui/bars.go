package ui

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func ThinBar(w int, frac, t float64, tone, bg color.Color) string {
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
		b.WriteString(Paint(G.Heavy, c, bg, false))
	}
	return b.String()
}

// all meter motion in one switch: the rippling surface, the gloss sweep and the grow-in
const MeterMotion = true

const meterGrow = 900 * time.Millisecond

var (
	lowers = [...]string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	lefts  = [...]string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}
	white  = color.RGBA{255, 255, 255, 255}
)

// a piece of meter text: 0 strong, 1 normal, 2 subtle
type Seg struct {
	Text string
	Tone int
}

func MeterMoving() bool { return MeterMotion && !Reduced() }

// 0..1 into the grow-in that starts at since
func MeterGrow(since, now time.Time) float64 {
	if !MeterMoving() {
		return 1
	}
	return Ease(0, 1, since, now, meterGrow)
}

func MeterGrowing(since, now time.Time) bool { return MeterMoving() && now.Sub(since) < meterGrow }

// three rows of water fill with its labels inside, inked to read on both the fill and the track
// a drive's space as a meter: used of total and the share on the left, free on the right
func SpaceMeter(w int, d api.Drive, t, grow float64, under color.Color) []string {
	sp := d.Space
	if sp == nil || sp.Total <= 0 {
		why := "Connect to see it"
		if d.State == api.Connected {
			why = "Not reported"
		}
		return Meter(w, 0, t, false, under, []Seg{{Text: "Free space unknown", Tone: 1}}, []Seg{{Text: why, Tone: 2}})
	}
	used := 1 - float64(sp.Free)/float64(sp.Total)
	left := []Seg{{Text: core.Size(int64(float64(sp.Total-sp.Free)*grow)) + " used"}, {Text: " of " + core.Size(sp.Total), Tone: 1},
		{Text: "  " + strconv.Itoa(int(used*grow*100+.5)) + "%", Tone: 2}}
	return Meter(w, used*grow, t, used > .9, under, left, []Seg{{Text: core.Size(sp.Free) + " free", Tone: 1}})
}

// under is what the meter sits on, nil for the screen
func Meter(w int, frac, t float64, warn bool, under color.Color, left, right []Seg) []string {
	p := P()
	out := make([]string, 3)
	if w <= 0 {
		return out
	}
	moving := MeterMoving()
	if under == nil {
		under = p.Bg
	}
	track := Mix(under, p.Text, .07)
	lo, hi := RampAt(.5), RampAt(.85)
	if !p.Dark {
		lo, hi = RampAt(.62), RampAt(.95)
	}
	if warn {
		lo, hi = Mix(p.Bg, p.Buoy, .6), p.Buoy
	}
	eighths := int(math.Round(math.Max(0, math.Min(1, frac)) * float64(w*8)))
	full, part := eighths/8, eighths%8
	edge := full - 1
	if part > 0 {
		edge = full
	}
	gloss := math.Mod(t/2.6, 1)*float64(full+20) - 10
	fill := func(x, row int) color.Color {
		c := Mix(lo, hi, float64(x)/math.Max(1, float64(w-1)))
		switch row {
		case 0:
			c = Mix(c, white, .1)
		case 2:
			c = Mix(c, p.Bg, .18)
		}
		if moving {
			c = Mix(c, white, .32*math.Exp(-math.Pow((float64(x)-gloss)/4, 2)))
		}
		if x == edge {
			c = Mix(c, white, .25)
		}
		return c
	}

	ink := make([]rune, w)
	tone := make([]int, w)
	for i := range ink {
		ink[i], tone[i] = ' ', -1
	}
	place := func(x int, segs []Seg) {
		for _, sg := range segs {
			for _, r := range sg.Text {
				if x >= 0 && x < w {
					ink[x], tone[x] = r, sg.Tone
				}
				x++
			}
		}
	}
	width := func(segs []Seg) int {
		n := 0
		for _, sg := range segs {
			n += len([]rune(sg.Text))
		}
		return n
	}
	place(2, left)
	if rw := width(right); w-2-rw >= 2+width(left)+3 {
		place(w-2-rw, right)
	}

	for row := 0; row < 3; row++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			in := x < full || x == full && part >= 4
			if row == 1 && tone[x] >= 0 {
				bg := track
				fgs := [3]color.Color{p.Text, p.Muted, p.Faint}
				if in {
					bg = fill(x, 1)
					fgs = [3]color.Color{p.OnAccent, Mix(bg, p.OnAccent, .78), Mix(bg, p.OnAccent, .58)}
				}
				b.WriteString(Paint(string(ink[x]), fgs[tone[x]], bg, tone[x] == 0))
				continue
			}
			switch {
			case x == full && part > 0:
				b.WriteString(Paint(lefts[part], fill(x, row), track, false))
			case x < full && row == 0 && moving:
				lvl := int(math.Round(6.6 + 1.1*math.Sin(float64(x)*.45-t*3.2) + .5*math.Sin(float64(x)*1.1+t*2.1)))
				lvl = max(4, min(8, lvl))
				if lvl == 8 {
					b.WriteString(Paint(" ", nil, fill(x, row), false))
				} else {
					b.WriteString(Paint(lowers[lvl], fill(x, row), track, false))
				}
			case x < full:
				b.WriteString(Paint(" ", nil, fill(x, row), false))
			default:
				b.WriteString(Paint(" ", nil, track, false))
			}
		}
		out[row] = b.String()
	}
	return out
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

// braille area chart of the last two minutes, newest on the right: a teal body brightening upwards under a glowing crest, a one-dot water line at rest
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
	idle, crest := n < 3 || top <= 0, RampAt(1)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		body := RampAt(.45 + .4*(1-float64(r)/math.Max(1, float64(rows-1))))
		lvl := float64((rows - 1 - r) * 4)
		for i := 0; i < w; i++ {
			hs := [2]float64{height(2 * i), height(2*i + 1)}
			bits := 0
			for dx, h := range hs {
				for dy := 0; dy < 4; dy++ {
					if lvl+float64(3-dy) < h {
						bits |= dots[dx][dy]
					}
				}
			}
			if bits == 0 {
				b.WriteByte(' ')
				continue
			}
			c := body
			if peak := math.Max(hs[0], hs[1]); !idle && peak > lvl && peak <= lvl+4 {
				c = crest
			}
			b.WriteString(Paint(string(rune(0x2800+bits)), c, nil, false))
		}
		out[r] = b.String()
	}
	return out
}
