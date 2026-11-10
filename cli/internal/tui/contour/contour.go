// Package contour draws a topographic map whose terrain slowly breathes: fine contour lines in braille dots that
// drift, merge and split, every fourth line brighter, over a faint tint of the land's height
package contour

import (
	"image/color"
	"math"
	"strconv"
)

const (
	levels = 9.0  // contour lines across the height range
	thick  = .34  // how far from a level, in steps of the slope, a dot still counts as on the line
	fps    = 20.0 // Step is one frame at this rate
)

// Palette is what the map is drawn in: the screen behind it, the faint and strong line colours and the land's tint
type Palette struct{ Bg, Lo, Hi, Tint color.Color }

type rgb struct{ r, g, b float64 }

func toRGB(c color.Color) rgb {
	if c == nil {
		return rgb{}
	}
	r, g, b, _ := c.RGBA()
	return rgb{float64(r >> 8), float64(g >> 8), float64(b >> 8)}
}

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func smooth(a, b, x float64) float64 { t := clamp((x - a) / (b - a)); return t * t * (3 - 2*t) }

// Map is the animated band, w cells wide and rows tall; each cell holds 2 by 4 braille dots
type Map struct {
	w, rows          int
	t                float64
	still            bool
	bg, lo, hi, tint rgb
	field            []float64
	buf              []byte
}

// New makes a map w cells by rows; nil when either is not positive
func New(w, rows int) *Map { return NewAt(w, rows, 0) }

// NewAt starts the terrain at time t, for repeatable frames
func NewAt(w, rows int, t float64) *Map {
	if w <= 0 || rows <= 0 {
		return nil
	}
	m := &Map{w: w, rows: rows, t: t, field: make([]float64, w*2*rows*4)}
	m.SetPalette(Palette{Bg: color.Black, Lo: color.RGBA{0x0A, 0x4E, 0x66, 255}, Hi: color.RGBA{0x9F, 0xF2, 0xEA, 255}, Tint: color.RGBA{0x04, 0x16, 0x1D, 255}})
	return m
}

func (m *Map) SetPalette(p Palette) {
	if m != nil {
		m.bg, m.lo, m.hi, m.tint = toRGB(p.Bg), toRGB(p.Lo), toRGB(p.Hi), toRGB(p.Tint)
	}
}

// SetStill freezes the terrain, for reduced motion
func (m *Map) SetStill(on bool) {
	if m != nil {
		m.still = on
	}
}

func (m *Map) Size() (int, int) {
	if m == nil {
		return 0, 0
	}
	return m.w, m.rows
}

// Step moves the terrain one frame
func (m *Map) Step() {
	if m != nil && !m.still {
		m.t += 1 / fps
	}
}

// braille dot bits by column and row inside a cell
var bit = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// Render draws the map as one line per row, each exactly w cells wide
func Render(m *Map) []string {
	if m == nil {
		return nil
	}
	W, H := m.w*2, m.rows*4
	z := m.t * .045
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			m.field[y*W+x] = fbm(float64(x)*.011, float64(y)*.03, z)*1.1 + float64(y)/float64(H)*.5
		}
	}
	b := m.buf[:0]
	lines := make([]string, m.rows)
	for cy := 0; cy < m.rows; cy++ {
		start := len(b)
		var lastFg, lastBg rgb
		first := true
		for cx := 0; cx < m.w; cx++ {
			bg := mix(m.bg, m.tint, smooth(0, 5, float64(cy))*(.5+.5*m.field[cy*4*W+cx*2]))
			var bits uint8
			fg, best := bg, -1.0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					c, k, ok := m.line(cx*2+dx, cy*4+dy, W, H)
					if !ok {
						continue
					}
					bits |= bit[dx][dy]
					if k > best {
						fg, best = c, k
					}
				}
			}
			if first || bg != lastBg {
				b = appendSGR(b, 48, bg)
				lastBg = bg
			}
			if bits == 0 {
				b = append(b, ' ')
			} else {
				if first || fg != lastFg {
					b = appendSGR(b, 38, fg)
					lastFg = fg
				}
				b = append(b, string(rune(0x2800+int(bits)))...)
			}
			first = false
		}
		b = append(b, "\x1b[0m"...)
		lines[cy] = string(b[start:])
	}
	m.buf = b
	return lines
}

// line says whether dot x, y sits on a contour line, its colour, and how strong it is
func (m *Map) line(x, y, W, H int) (rgb, float64, bool) {
	if x < 1 || y < 1 || x >= W-1 || y >= H-1 {
		return rgb{}, 0, false
	}
	i := y*W + x
	v := m.field[i] * levels
	gx, gy := (m.field[i+1]-m.field[i-1])*levels*.5, (m.field[i+W]-m.field[i-W])*levels*.5
	g := math.Hypot(gx, gy) + 1e-4
	lvl := math.Round(v)
	if math.Abs(v-lvl)/g > thick {
		return rgb{}, 0, false
	}
	major := int(lvl)%4 == 0
	k := clamp((lvl + 2) / (levels + 2))
	c := mix(m.lo, m.hi, k*.7)
	strength, reach := .8, k
	if major {
		c, strength, reach = mix(c, m.hi, .55), 1, k+1
	}
	return mix(m.bg, c, smooth(0, 10, float64(y))*strength), reach, true
}

func appendSGR(b []byte, kind int, c rgb) []byte {
	b = append(b, "\x1b["...)
	b = strconv.AppendInt(b, int64(kind), 10)
	b = append(b, ";2;"...)
	b = strconv.AppendInt(b, int64(c.r+.5), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(c.g+.5), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(c.b+.5), 10)
	return append(b, 'm')
}
