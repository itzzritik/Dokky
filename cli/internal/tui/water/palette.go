package water

import (
	"fmt"
	"image/color"
)

// Palette is the water's colours from the deepest water up to white foam.
type Palette struct {
	Bg      color.Color // what the band melts into, usually the terminal background
	Deep    color.Color // the bottom of the band
	Body    color.Color // open water
	Shallow color.Color // just under the surface
	Light   color.Color // the lit skin under the waterline, caustics and bubbles
	Foam    color.Color // glints, foam and spray
}

const (
	levels = 48 // steps along the water ramp
	fades  = 8  // steps from the background to full water, for edges and the top
)

// where each palette colour sits on the 0..1 brightness the sim produces
var stops = [5]float64{0, .3, .56, .8, 1}

type rgb struct{ r, g, b uint8 }

func toRGB(c color.Color) rgb {
	if c == nil {
		return rgb{}
	}
	r, g, b, _ := c.RGBA()
	return rgb{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func mix(a, b rgb, t float64) rgb {
	t = clamp(t, 0, 1)
	f := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t + .5) }
	return rgb{f(a.r, b.r), f(a.g, b.g), f(a.b, b.b)}
}

func (c rgb) lum() float64 { return .2126*float64(c.r) + .7152*float64(c.g) + .0722*float64(c.b) }

func (c rgb) RGBA() color.RGBA { return color.RGBA{c.r, c.g, c.b, 255} }

// fallback until SetPalette is called: teal water on black
var fallback = Palette{
	Bg:      color.RGBA{0, 0, 0, 255},
	Deep:    color.RGBA{0x03, 0x14, 0x1d, 255},
	Body:    color.RGBA{0x0a, 0x4e, 0x66, 255},
	Shallow: color.RGBA{0x14, 0x95, 0xad, 255},
	Light:   color.RGBA{0x74, 0xe8, 0xe1, 255},
	Foam:    color.RGBA{0xf0, 0xff, 0xfc, 255},
}

type ramp struct {
	key    [6]rgb
	fg, bg [levels * fades]string
	spray  rgb // droplets must read against the background, so foam on dark and teal on light
}

func (r *ramp) build(p Palette) {
	r.key = [6]rgb{toRGB(p.Bg), toRGB(p.Deep), toRGB(p.Body), toRGB(p.Shallow), toRGB(p.Light), toRGB(p.Foam)}
	bg := r.key[0]
	for l := 0; l < levels; l++ {
		c := r.at(float64(l) / (levels - 1))
		for f := 1; f < fades; f++ {
			m := mix(bg, c, float64(f)/(fades-1))
			r.fg[f*levels+l] = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", m.r, m.g, m.b)
			r.bg[f*levels+l] = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", m.r, m.g, m.b)
		}
	}
	r.spray = r.key[5]
	if bg.lum() > 128 {
		r.spray = mix(r.key[2], r.key[3], .5)
	}
}

func (r *ramp) at(v float64) rgb {
	v = clamp(v, 0, 1)
	for i := 1; i < len(stops); i++ {
		if v <= stops[i] {
			return mix(r.key[i], r.key[i+1], (v-stops[i-1])/(stops[i]-stops[i-1]))
		}
	}
	return r.key[5]
}

// SetPalette recolours the water; it is cheap when nothing changed, so call it every frame.
func (t *Water) SetPalette(p Palette) {
	if t == nil {
		return
	}
	k := [6]rgb{toRGB(p.Bg), toRGB(p.Deep), toRGB(p.Body), toRGB(p.Shallow), toRGB(p.Light), toRGB(p.Foam)}
	if t.ramp != nil && t.ramp.key == k {
		return
	}
	t.ramp = new(ramp)
	t.ramp.build(p)
}
