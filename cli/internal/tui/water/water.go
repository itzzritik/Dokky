// Package water draws a band of sunlit water for a terminal: Gerstner swells
// and a ripple field shape a moving waterline, light, caustics and foam are
// shaded per pixel in half blocks, and spray comes back as data so a screen
// can lay it around its own content.
package water

import (
	"math"
	"math/rand/v2"
)

const (
	dt       = 1 / 30.0 // one frame of the ui tick
	gravity  = 9.4      // px/s², so long swells outrun short ones as on real water
	zrows    = 24       // ripple field rows behind the glass, half a pixel apart
	sheens   = 6        // depth samples of the lit skin under the waterline
	sheenD   = 2.6      // pixels of that skin
	zstep    = 1.5      // pixels of surface behind the glass per skin sample
	depths   = 4        // depths where refracted sunlight is gathered into caustics
	ripH     = .8       // px the waterline lifts per unit of ripple field
	maxBub   = 48
	maxDrops = 96
)

var causticAt = [depths]float64{1.5, 5, 10, 18}

// wavelength in px, height in px, heading in radians (0 runs right, along the glass), crest sharpness
var swellSpec = [...]struct{ lambda, amp, heading, q float64 }{
	{74, 1.9, .1, 1.25},
	{46, 1.15, 2.8, 1.25},
	{33, .7, -.45, 1.2},
	{21, .45, .6, 1.1},
	{13, .26, -2.3, 1},
	{8, .13, 1.1, .8},
}

type swell struct{ kx, kz, k, amp, qa, omega, phase float64 }

type bubble struct{ x, y, vy, ph, a float64 }

type drop struct {
	x, y, vx, vy, life, dec float64
	mist                    bool
}

// Water is one band of water; build it with New and drive it with Step once per ui tick.
type Water struct {
	w, rows, sw, px int // cells, supersampled columns, pixel rows (two per cell)
	rng             *rand.Rand
	energy, target  float64
	still           bool
	rest            float64 // resting waterline, px from the top
	size            float64 // swells shrink on short bands
	drift           float64 // foam and mist carried by the wind, px
	clock           float64 // seconds of motion, drives the caustic drift

	swells    [len(swellSpec)]swell
	surf      []float64 // waterline per supersampled column, px from the top
	slope     []float64
	foam, tmp []float64
	cur, prev []float64 // ripple field, sw x zrows
	sheen     []float64 // lit skin brightness, w x sheens
	light     []float64 // caustic density, depths x w
	crest     []float64 // how near each pixel column is to a crest, for glints
	lace      []float64 // static noise that foam dissolves through
	net       []float32 // tiling caustic network, tile x tile
	bubbles   []bubble
	drops     []drop
	out       []Drop

	val, cov  []float64 // per pixel brightness and coverage
	lvl, fade []uint8
	ramp      *ramp
	buf       []byte
	ends      []int
}

// New makes a band w cells wide and rows cells tall, or nil when empty.
func New(w, rows int) *Water { return NewSeeded(w, rows, rand.Uint64()) }

// NewSeeded is New with a fixed seed, so frames repeat exactly.
func NewSeeded(w, rows int, seed uint64) *Water {
	if w <= 0 || rows <= 0 {
		return nil
	}
	t := &Water{w: w, rows: rows, sw: 2 * w, px: 2 * rows, energy: 1, target: 1}
	t.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	t.rest = .4 * float64(t.px)
	t.size = min(1, float64(t.px)/24)
	for i, s := range swellSpec {
		lambda := s.lambda * (.88 + .24*t.rng.Float64())
		k := 2 * math.Pi / lambda
		h := s.heading + (t.rng.Float64()-.5)*.3
		t.swells[i] = swell{kx: k * math.Cos(h), kz: k * math.Sin(h), k: k, amp: s.amp, qa: s.q * s.amp,
			omega: math.Sqrt(gravity * k), phase: t.rng.Float64() * 2 * math.Pi}
	}
	t.surf, t.slope = make([]float64, t.sw), make([]float64, t.sw)
	t.foam, t.tmp = make([]float64, t.sw), make([]float64, t.sw)
	t.cur, t.prev = make([]float64, t.sw*zrows), make([]float64, t.sw*zrows)
	t.sheen, t.crest = make([]float64, w*sheens), make([]float64, w)
	t.light = make([]float64, depths*w)
	t.lace = make([]float64, t.sw*8)
	for i := range t.lace {
		t.lace[i] = t.rng.Float64()
	}
	blur(t.lace, t.sw, 8)
	t.net = network(t.rng)
	t.bubbles = make([]bubble, 0, maxBub)
	t.drops = make([]drop, 0, maxDrops)
	t.out = make([]Drop, 0, maxDrops)
	t.val, t.cov = make([]float64, w*t.px), make([]float64, w*t.px)
	t.lvl, t.fade = make([]uint8, w*t.px), make([]uint8, w*t.px)
	t.ends = make([]int, rows)
	t.SetPalette(fallback)
	for range 90 {
		t.advance()
	}
	t.shade()
	return t
}

// one box blur pass over a wrapping grid, so the foam lace has soft edges
func blur(g []float64, w, h int) {
	src := append([]float64(nil), g...)
	for y := range h {
		for x := range w {
			s := 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					s += src[((y+dy+h)%h)*w+(x+dx+w)%w]
				}
			}
			g[y*w+x] = s / 9
		}
	}
	lo, hi := 1.0, 0.0
	for _, v := range g {
		lo, hi = min(lo, v), max(hi, v)
	}
	for i := range g {
		g[i] = (g[i] - lo) / max(hi-lo, 1e-9)
	}
}

// Size is the band in cells.
func (t *Water) Size() (int, int) {
	if t == nil {
		return 0, 0
	}
	return t.w, t.rows
}

// SetEnergy makes the water calmer below 1 and livelier above it; it eases in over a second.
func (t *Water) SetEnergy(e float64) {
	if t != nil {
		t.target = clamp(e, 0, 3)
	}
}

// SetStill freezes the water to a calm frame, for reduced motion.
func (t *Water) SetStill(on bool) {
	if t == nil || t.still == on {
		return
	}
	t.still = on
	if !on {
		return
	}
	t.energy = .45
	clear(t.foam)
	clear(t.cur)
	clear(t.prev)
	t.bubbles, t.drops = t.bubbles[:0], t.drops[:0]
	t.advance()
	t.shade()
}

// Step moves the water on by one frame.
func (t *Water) Step() {
	if t == nil || t.still {
		return
	}
	t.advance()
	t.shade()
}

func (t *Water) amp() float64 { return (.5 + .5*t.energy) * t.size }

func (t *Water) advance() {
	if !t.still {
		t.energy += (t.target - t.energy) * .05
	}
	e := t.energy
	speed := .9 + .1*e
	for i := range t.swells {
		t.swells[i].phase -= t.swells[i].omega * dt * speed
	}
	t.drift += .05
	t.clock += dt * speed
	if !t.still {
		t.wind(e)
	}
	// two passes a frame so rings spread at the pace of real ripples
	t.ripple()
	t.ripple()
	t.waterline()
	t.caustics()
	t.skin()
	if !t.still {
		t.stepBubbles(e)
		t.stepDrops()
	}
}

// small random gusts keep a fine chop alive, rarer drops send out rings
func (t *Water) wind(e float64) {
	n := float64(t.sw) / 60 * (.4 + .6*e)
	for ; n > 0; n-- {
		if n < 1 && t.rng.Float64() > n {
			break
		}
		t.poke(t.rng.Float64()*float64(t.sw), t.rng.IntN(zrows), (t.rng.Float64()-.5)*.7)
	}
	if t.rng.Float64() < .004*float64(t.w)/100*(.5+.5*e) {
		x, z := t.rng.Float64()*float64(t.sw), t.rng.IntN(zrows/2)
		t.poke(x, z, -2.2)
		if z < 4 {
			t.splash(x/2, 2+t.rng.IntN(3))
		}
	}
}

// poke pushes the ripple field down at supersampled column x and row z
func (t *Water) poke(x float64, z int, a float64) {
	i := int(x)
	sw := t.sw
	at := func(i, z int) *float64 { return &t.cur[min(zrows-1, max(0, z))*sw+(i+sw)%sw] }
	*at(i, z) += a
	*at(i-1, z) += a * .5
	*at(i+1, z) += a * .5
	*at(i, z-1) += a * .5
	*at(i, z+1) += a * .5
}

// one step of the 2d wave equation; the glass at z=0 reflects, the far rows soak waves up
func (t *Water) ripple() {
	sw, c, p := t.sw, t.cur, t.prev
	for z := 0; z < zrows; z++ {
		row := c[z*sw : (z+1)*sw]
		var up, dn []float64
		if z > 0 {
			up = c[(z-1)*sw : z*sw]
		} else {
			up = c[sw : 2*sw]
		}
		if z < zrows-1 {
			dn = c[(z+1)*sw : (z+2)*sw]
		}
		damp := .985
		if z > zrows-6 {
			damp = .985 - float64(z-(zrows-6))*.03
		}
		out := p[z*sw : (z+1)*sw]
		for i := 0; i < sw; i++ {
			l, r := i-1, i+1
			if l < 0 {
				l = sw - 1
			}
			if r == sw {
				r = 0
			}
			s := row[l] + row[r] + up[i]
			if dn != nil {
				s += dn[i]
			}
			out[i] = (s*.5 - out[i]) * damp
		}
	}
	t.cur, t.prev = p, c
}

// the waterline is the swells at the glass, inverted through the gerstner sideways motion so crests come out sharp
func (t *Water) waterline() {
	a, e := t.amp(), t.energy
	for i := 0; i < t.sw; i++ {
		x := (float64(i) + .5) / 2
		x0 := x
		for range 3 {
			dx := 0.0
			for _, s := range t.swells {
				dx += s.qa * s.kx / s.k * math.Sin(s.kx*x0+s.phase)
			}
			x0 = x + dx*a
		}
		eta, deta, fold := 0.0, 0.0, 0.0
		for _, s := range t.swells {
			sn, cs := math.Sincos(s.kx*x0 + s.phase)
			eta += s.amp * cs
			deta -= s.amp * s.kx * sn
			fold += s.qa * s.kx * s.kx / s.k * cs
		}
		j := max(.08, 1-fold*a)
		r := t.cur[i]
		l, rr := t.cur[(i-1+t.sw)%t.sw], t.cur[(i+1)%t.sw]
		t.surf[i] = t.rest - eta*a - r*ripH
		t.slope[i] = deta*a/j - (rr-l)*ripH
		// whitecaps form where the crest folds over itself
		if !t.still && j < .7 && eta > 0 {
			f := (.7 - j) * (.6 + .4*e)
			t.foam[i] = min(1, t.foam[i]+f*.35)
			if t.rng.Float64() < f*.05*e {
				t.spray(x, t.surf[i]-.4, e)
			}
			if t.rng.Float64() < f*.012 {
				t.addBubble(x, t.surf[i]+1+t.rng.Float64()*2)
			}
		}
	}
	// foam drifts with the wind and dissolves
	if !t.still {
		sh := .1
		for i := range t.foam {
			l := t.foam[(i-1+t.sw)%t.sw]
			t.tmp[i] = (t.foam[i]*(1-sh) + l*sh) * .982
		}
		t.foam, t.tmp = t.tmp, t.foam
	}
	for x := 0; x < t.w; x++ {
		c := (t.rest - (t.surf[2*x]+t.surf[2*x+1])/2) / (2.6 * max(a, .3))
		t.crest[x] = clamp(c, 0, 1)
	}
}

// the skin under the waterline shows the surface further back, shaded by its normal with light from above
func (t *Water) skin() {
	a := t.amp()
	lx, ly, lz := norm(-.35, 1, -.25)
	hx, hy, hz := norm(.1, 1, -.15)
	for x := 0; x < t.w; x++ {
		px := float64(x) + .5
		for j := range sheens {
			z := float64(j) * zstep
			gx, gz := 0.0, 0.0
			for _, s := range t.swells {
				sn := math.Sin(s.kx*px + s.kz*z + s.phase)
				gx -= s.amp * s.kx * sn
				gz -= s.amp * s.kz * sn
			}
			gx, gz = gx*a, gz*a
			zi := min(zrows-2, int(z*2))
			i := 2*x + 1
			c := t.cur[zi*t.sw:]
			gx -= (c[(i+1)%t.sw] - c[i-1]) * ripH
			gz -= (c[t.sw+i] - c[i]) * 2 * ripH
			nx, ny, nz := norm(-gx, 1, -gz)
			lam := nx*lx + ny*ly + nz*lz - ly
			g := smooth(.9982, .9996, nx*hx+ny*hy+nz*hz) * (.3 + .7*t.crest[x])
			t.sheen[x*sheens+j] = .7 + clamp(2.4*lam, -.2, .16) + .4*g
		}
	}
}

func (t *Water) addBubble(x, y float64) {
	if len(t.bubbles) < maxBub {
		t.bubbles = append(t.bubbles, bubble{x: x, y: y, vy: -.04 - t.rng.Float64()*.06, ph: t.rng.Float64() * 6.3})
	}
}

func (t *Water) stepBubbles(e float64) {
	if t.rng.Float64() < .02*float64(t.w)/100*(.6+.4*e) {
		t.addBubble(t.rng.Float64()*float64(t.w), float64(t.px)-.5-t.rng.Float64()*3)
	}
	kept := t.bubbles[:0]
	for _, b := range t.bubbles {
		b.vy = max(-.22, b.vy-.0025)
		b.y += b.vy
		b.ph += .22
		b.x += math.Sin(b.ph) * .07
		b.a = min(1, b.a+.08)
		if b.y <= t.surfAt(b.x)+.6 {
			t.poke(b.x*2, 1, -.25)
			if t.rng.Float64() < .3 {
				t.mist(b.x, t.surfAt(b.x)-.5)
			}
			continue
		}
		kept = append(kept, b)
	}
	t.bubbles = kept
}

func (t *Water) surfAt(x float64) float64 {
	i := int(x * 2)
	return t.surf[min(t.sw-1, max(0, i))]
}

func (t *Water) spray(x, y, e float64) {
	if len(t.drops) < maxDrops {
		t.drops = append(t.drops, drop{x: x, y: y, vx: .05 + (t.rng.Float64()-.3)*.25, vy: -(.35 + .45*t.rng.Float64()) * (.7 + .3*e),
			life: 1, dec: 1 / (30 * (1.5 + t.rng.Float64()))})
	}
	if t.rng.Float64() < .5 {
		t.mist(x, y)
	}
}

func (t *Water) splash(x float64, n int) {
	for range n {
		if len(t.drops) < maxDrops {
			t.drops = append(t.drops, drop{x: x, y: t.surfAt(x) - .5, vx: (t.rng.Float64() - .5) * .35, vy: -.3 - .3*t.rng.Float64(),
				life: 1, dec: 1 / (30 * (1 + t.rng.Float64()))})
		}
	}
}

func (t *Water) mist(x, y float64) {
	if len(t.drops) < maxDrops {
		t.drops = append(t.drops, drop{x: x, y: y, vx: .03 + t.rng.Float64()*.05, vy: -.03 - t.rng.Float64()*.05,
			life: .7, dec: 1 / (30 * (1.5 + 2*t.rng.Float64())), mist: true})
	}
}

func (t *Water) stepDrops() {
	kept := t.drops[:0]
	for _, d := range t.drops {
		if d.mist {
			d.vy -= .0008
		} else {
			d.vy += .045
		}
		d.x += d.vx
		d.y += d.vy
		d.life -= d.dec
		if d.x < 0 || d.x >= float64(t.w) || d.life <= 0 {
			continue
		}
		if !d.mist && d.vy > 0 && d.y >= t.surfAt(d.x) {
			t.poke(d.x*2, 0, -.6)
			continue
		}
		kept = append(kept, d)
	}
	t.drops = kept
}

func norm(x, y, z float64) (float64, float64, float64) {
	l := 1 / math.Sqrt(x*x+y*y+z*z)
	return x * l, y * l, z * l
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(hi, v)) }

func smooth(a, b, x float64) float64 {
	x = clamp((x-a)/(b-a), 0, 1)
	return x * x * (3 - 2*x)
}
