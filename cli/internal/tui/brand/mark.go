package brand

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var glyphs = map[rune][]string{
	'd': {"......##", "......##", ".#######", "########", "##....##", "##....##", "########", ".#######", "", ""},
	'o': {"", "", ".######.", "########", "##....##", "##....##", "########", ".######.", "", ""},
	'k': {"##.....", "##.....", "##...##", "##..##.", "#####..", "##.##..", "##..##.", "##...##", "", ""},
	'y': {"", "", "##....##", "##....##", "##....##", "##....##", "########", ".#######", "......##", ".######."},
}

const (
	W, H = 46, 5
	rest = 3.4
)

var pixels, columns = func() ([W][2 * H]bool, []int) {
	var px [W][2 * H]bool
	var cols []int
	x := 0
	for _, ch := range "dokky" {
		g, w := glyphs[ch], 0
		for y, row := range g {
			w = max(w, len(row))
			for i, c := range row {
				if c == '#' {
					px[x+i][y] = true
				}
			}
		}
		x += w + 2
	}
	for i := range px {
		for _, on := range px[i] {
			if on {
				cols = append(cols, i)
				break
			}
		}
	}
	return px, cols
}()

type bubble struct {
	x    int
	y, v float64
}

type Mark struct {
	start, last time.Time
	bubbles     []bubble
}

func NewMark(now time.Time) *Mark { return &Mark{start: now, last: now} }

func (m *Mark) Animating(time.Time) bool { return !ui.Reduced() }

func (m *Mark) level(a float64) float64 {
	if ui.Reduced() {
		return rest
	}
	return math.Max(1, rest+(10.5-rest)*math.Exp(-a*1.9)*math.Cos(a*4.6))
}

func (m *Mark) step(now time.Time, lvl float64) {
	dt := now.Sub(m.last).Seconds()
	m.last = now
	if ui.Reduced() || dt <= 0 {
		return
	}
	if rand.Float64() < .06*dt/.033 {
		m.bubbles = append(m.bubbles, bubble{x: columns[rand.IntN(len(columns))], y: 9.5, v: 2.5 + 3*rand.Float64()})
	}
	kept := m.bubbles[:0]
	for _, b := range m.bubbles {
		b.y -= b.v * dt
		if b.y > lvl {
			kept = append(kept, b)
		}
	}
	m.bubbles = kept
}

func (m *Mark) View(now time.Time) string {
	p := ui.P()
	t := now.Sub(m.start).Seconds()
	lvl := m.level(t)
	m.step(now, lvl)
	motion := 1.0
	if ui.Reduced() {
		motion = 0
	}
	glass := ui.Mix(p.Bg, p.Ramp[1], .5)
	colour := func(x, y int) color.Color {
		if !pixels[x][y] {
			return nil
		}
		s := lvl + motion*(.6*math.Sin(float64(x)*.42+t*2.1)+.35*math.Sin(float64(x)*.17-t*1.37))
		if float64(y) < s-.5 {
			return glass
		}
		for _, b := range m.bubbles {
			if b.x == x && int(math.Round(b.y)) == y {
				return ui.Mix(p.Ramp[4], p.Ramp[5], .6)
			}
		}
		if d := float64(y) - s; d >= .6 {
			return ui.RampAt(.86 - d/7)
		}
		return p.Ramp[5]
	}
	rows := make([]string, H)
	for cy := range rows {
		var b strings.Builder
		for x := 0; x < W; x++ {
			top, bot := colour(x, 2*cy), colour(x, 2*cy+1)
			switch {
			case ui.G.ASCII && (top != nil || bot != nil):
				b.WriteString(ui.Paint("#", cmpColor(top, bot), nil, true))
			case top != nil:
				b.WriteString(ui.Paint("▀", top, bot, false))
			case bot != nil:
				b.WriteString(ui.Paint("▄", bot, nil, false))
			default:
				b.WriteByte(' ')
			}
		}
		rows[cy] = b.String()
	}
	return strings.Join(rows, "\n")
}

func cmpColor(a, b color.Color) color.Color {
	if a != nil {
		return a
	}
	return b
}
