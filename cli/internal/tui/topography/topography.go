// Package topography is React Bits' Topography for the terminal: a contour map of mountains seen from a camera
// gliding over them, lines fading into the distance, every fifth line brighter, drawn in braille dots across a
// whole screen. Rectangles set with Calm, where text sits, keep the lines to a whisper so that text stays legible
package topography

import (
	"image"
	"image/color"
	"math"
	"strconv"
)

// the scene, as in the React Bits component with its usage example's props
const (
	fps        = 20.0
	height     = 1.7  // how tall the mountains rise
	frequency  = .45  // the size of the landforms
	lines      = 22.0 // contour lines up the tallest mountain
	pitch      = 32 * math.Pi / 180
	orbit      = 9.0
	speed      = .25 * .8 // how fast the camera glides forward
	morphRate  = .2 * .12 // how fast the land reshapes
	fog        = .35
	foreground = .2  // the height the land keeps as it reaches the camera
	intro      = 2.8 // seconds the land takes to rise on first show
)

// Palette is what the map is drawn in; on a light screen the lines are ink instead of light
type Palette struct {
	Bg, Line color.Color
	Light    bool
}

type rgb struct{ r, g, b float64 }

func toRGB(c color.Color) rgb {
	if c == nil {
		return rgb{}
	}
	r, g, b, _ := c.RGBA()
	return rgb{float64(r>>8) / 255, float64(g>>8) / 255, float64(b>>8) / 255}
}

// Map is the animated terrain, w cells wide and rows tall; each cell holds 2 by 4 braille dots
type Map struct {
	w, rows int
	t, age  float64 // the camera's clock, and time since the map first showed for the rise
	still   bool
	pal     Palette
	bg, ink rgb
	calm    []image.Rectangle
	h, d    []float64 // per dot: the land's height and its depth from the camera, NaN for none
	lit     []float64
	top     []float64 // per column: the highest screen row drawn so far
	buf     []byte
}

func New(w, rows int) *Map { return NewAt(w, rows, 0) }

// NewAt starts the glide at time t, for repeatable frames
func NewAt(w, rows int, t float64) *Map {
	if w <= 0 || rows <= 0 {
		return nil
	}
	n := w * 2 * rows * 4
	m := &Map{w: w, rows: rows, t: t, h: make([]float64, n), d: make([]float64, n), lit: make([]float64, n), top: make([]float64, w*2)}
	m.SetPalette(Palette{Bg: color.Black, Line: color.RGBA{0x22, 0xB8, 0xCC, 255}})
	return m
}

func (m *Map) SetPalette(p Palette) {
	if m != nil {
		m.pal, m.bg, m.ink = p, toRGB(p.Bg), toRGB(p.Line)
	}
}

// SetStill freezes the glide and skips the rise, for reduced motion
func (m *Map) SetStill(on bool) {
	if m != nil {
		m.still = on
	}
}

// Calm lists the cell rectangles where text sits; lines fade to a whisper around them
func (m *Map) Calm(r []image.Rectangle) {
	if m != nil {
		m.calm = r
	}
}

func (m *Map) Size() (int, int) {
	if m == nil {
		return 0, 0
	}
	return m.w, m.rows
}

func (m *Map) Step() {
	if m == nil {
		return
	}
	m.age += 1 / fps
	if !m.still {
		m.t += 1 / fps
	}
}

// quiet is how much of a line shows at cell x, y: about a tenth beside text, all of it seven cells away
func (m *Map) quiet(x, y float64) float64 {
	d := math.Inf(1)
	for _, r := range m.calm {
		dx := math.Max(math.Max(float64(r.Min.X-2)-x, 0), x-float64(r.Max.X+1))
		dy := math.Max(math.Max(float64(r.Min.Y-1)-y, 0), y-float64(r.Max.Y))
		d = math.Min(d, math.Hypot(dx, dy*2))
	}
	if math.IsInf(d, 1) {
		return 1
	}
	return .1 + .9*smoother(d/7)
}

// project fills each dot with the height and depth of the land it shows, front to back down each column
func (m *Map) project() {
	W, H := m.w*2, m.rows*4
	for i := range m.h {
		m.h[i], m.d[i] = math.NaN(), 0
	}
	for i := range m.top {
		m.top[i] = float64(H)
	}
	rise := 1.0
	if !m.still {
		rise = smoother(m.age / intro)
	}
	sp, cp := math.Sin(pitch), math.Cos(pitch)
	eyeY, eyeZ := height*rise*.18+sp*orbit, -cp*orbit
	aspect := float64(W) / float64(H)
	lens := math.Min(.6, math.Max(.32, .4877/aspect))
	tn := 1 / lens
	travel, morph := m.t*speed, m.t*morphRate
	near0 := m.groundDepth(-1, eyeY, lens) * .9
	far := m.fadeEnd(eyeY, lens)
	amp := height * rise
	for col := 0; col < W; col++ {
		nx := (2*(float64(col)+.5)/float64(W) - 1) * aspect / tn
		prevY, prevH := math.NaN(), 0.0
		// from just below the screen's bottom edge out to the fog, a little further apart as the land recedes
		for D := near0 * .85; D < far; D *= 1.02 {
			Z := (D - eyeY*sp) / cp
			X := nx * D
			h0 := relief(X, eyeZ+Z+travel, frequency, morph*.37, morph*.23)
			settle := foreground + (1-foreground)*smoother((D-near0)/math.Max(orbit-near0, 1e-3))
			Y := h0*amp*settle - eyeY
			d := -Y*sp + Z*cp
			if d <= .05 {
				continue
			}
			sy := float64(H) / 2 * (1 - tn*(Y*cp+Z*sp)/d)
			if sy < m.top[col] {
				from := int(math.Ceil(sy))
				for r := max(from, 0); r < int(m.top[col]) && r < H; r++ {
					hh := h0
					if !math.IsNaN(prevY) && prevY > sy {
						hh = h0 + (prevH-h0)*clamp((float64(r)-sy)/(prevY-sy))
					}
					i := r*W + col
					m.h[i], m.d[i] = hh, D
				}
				m.top[col] = sy
			}
			prevY, prevH = sy, h0
		}
	}
}

// how far down the screen's bottom edge, ny -1, or top edge, ny 1, meets the ground
func (m *Map) groundDepth(ny, eyeY, lens float64) float64 {
	sp, cp := math.Sin(pitch), math.Cos(pitch)
	dy, dz := -sp+cp*ny*lens, cp+sp*ny*lens
	l := math.Hypot(dy, dz)
	dy, dz = dy/l, dz/l
	if dy > -1e-3 {
		return math.Inf(1)
	}
	return eyeY / -dy * (dy*-sp + dz*cp)
}

// where the land has faded into the fog, as React Bits sets it from the horizon and the fog amount
func (m *Map) fadeEnd(eyeY, lens float64) float64 {
	bottom, top := m.groundDepth(-1, eyeY, lens), math.Min(m.groundDepth(1, eyeY, lens), orbit*3.2)
	horizon := math.Max(top, math.Max(bottom*1.6, orbit*1.4))
	return horizon + (bottom*1.8-horizon)*fog
}

func clamp(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// Render draws the map as one line per row, each exactly w cells wide
func Render(m *Map) []string {
	if m == nil {
		return nil
	}
	m.project()
	W, H := m.w*2, m.rows*4
	sp := math.Sin(pitch)
	eyeY := height*smoother(m.age/intro)*.18 + sp*orbit
	aspect := float64(W) / float64(H)
	lens := math.Min(.6, math.Max(.32, .4877/aspect))
	end := m.fadeEnd(eyeY, lens)
	start := end*.7 + (m.groundDepth(-1, eyeY, lens)*.7-end*.7)*fog
	reveal := 1.0
	if !m.still {
		reveal = smoother(m.age / intro / .35)
	}
	// a dot is on a line where the band of heights changes from the dot to its right or below
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			i := y*W + x
			m.lit[i] = 0
			h := m.h[i]
			if math.IsNaN(h) {
				continue
			}
			band := math.Floor(h * lines)
			edge, major, slope := false, false, 0.0
			for _, j := range [2]int{i + 1, i + W} {
				if (j == i+1 && x == W-1) || j >= len(m.h) || math.IsNaN(m.h[j]) {
					continue
				}
				slope += math.Abs(m.h[j]-h) * lines
				if b := math.Floor(m.h[j] * lines); b != band {
					edge = true
					major = major || int(math.Max(b, band))%5 == 0
				}
			}
			if !edge {
				continue
			}
			// where lines crowd closer than a dot or two they fade, as the component's crowding does, or a steep
			// far slope turns into a solid patch of dots
			crowd := smoother((slope - .45) / .6)
			level := (.45 + .6*smoother(h/1.1)) * (1 + .6*b2f(major)) * (1 - .75*crowd)
			fade := 1 - smoother((m.d[i]-start)/math.Max(end-start, 1e-3))
			m.lit[i] = level * fade * reveal * m.quiet(float64(x)/2, float64(y)/4)
		}
	}
	b := m.buf[:0]
	out := make([]string, m.rows)
	for cy := 0; cy < m.rows; cy++ {
		from := len(b)
		var last rgb
		blank := true
		for cx := 0; cx < m.w; cx++ {
			var bits uint8
			best := 0.0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					l := m.lit[(cy*4+dy)*W+cx*2+dx]
					if l > .06 {
						bits |= dot[dx][dy]
						best = math.Max(best, l)
					}
				}
			}
			if bits == 0 {
				if !blank {
					b = append(b, "\x1b[39m"...)
					blank = true
				}
				b = append(b, ' ')
				continue
			}
			c := m.colour(best)
			if blank || c != last {
				b = appendFg(b, c)
				last, blank = c, false
			}
			b = append(b, string(rune(0x2800+int(bits)))...)
		}
		b = append(b, "\x1b[0m"...)
		out[cy] = string(b[from:])
	}
	m.buf = b
	return out
}

// light glows from the line colour toward white, as the component's tone mapping does; on a light screen it is ink
func (m *Map) colour(l float64) rgb {
	if m.pal.Light {
		k := (1 - math.Exp(-l*1.8)) * .92
		return rgb{m.bg.r + (m.ink.r-m.bg.r)*k, m.bg.g + (m.ink.g-m.bg.g)*k, m.bg.b + (m.ink.b-m.bg.b)*k}
	}
	f := func(c, bg float64) float64 { v := 1 - math.Exp(-c*l*1.8); return bg + (1-bg)*v }
	return rgb{f(m.ink.r, m.bg.r), f(m.ink.g, m.bg.g), f(m.ink.b, m.bg.b)}
}

func b2f(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

var dot = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

func appendFg(b []byte, c rgb) []byte {
	b = append(b, "\x1b[38;2;"...)
	b = strconv.AppendInt(b, int64(c.r*255+.5), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(c.g*255+.5), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(c.b*255+.5), 10)
	return append(b, 'm')
}
