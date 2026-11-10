package water

import (
	"image/color"
	"math"
)

// Render draws the band as one line per row, each exactly w cells wide.
func Render(t *Water) []string {
	if t == nil {
		return nil
	}
	const air = -1
	r := t.ramp
	b := t.buf[:0]
	ends := t.ends
	key := func(i int) int {
		if t.fade[i] == 0 {
			return air
		}
		return int(t.fade[i])*levels + int(t.lvl[i])
	}
	for row := 0; row < t.rows; row++ {
		fg, bg := air, air // air means the terminal default
		for x := 0; x < t.w; x++ {
			a, d := key(2*row*t.w+x), key((2*row+1)*t.w+x)
			switch {
			case a == air && d == air:
				if fg != air || bg != air {
					b = append(b, "\x1b[0m"...)
					fg, bg = air, air
				}
				b = append(b, ' ')
				continue
			case a == air:
				if bg != air {
					b = append(b, "\x1b[49m"...)
					bg = air
				}
				if fg != d {
					b = append(b, r.fg[d]...)
					fg = d
				}
				b = append(b, "▄"...)
				continue
			}
			if d == air {
				if bg != air {
					b = append(b, "\x1b[49m"...)
					bg = air
				}
			} else if bg != d {
				b = append(b, r.bg[d]...)
				bg = d
			}
			if a == d {
				b = append(b, ' ')
				continue
			}
			if fg != a {
				b = append(b, r.fg[a]...)
				fg = a
			}
			b = append(b, "▀"...)
		}
		b = append(b, "\x1b[0m"...)
		ends[row] = len(b)
	}
	t.buf = b
	s := string(b)
	lines := make([]string, t.rows)
	start := 0
	for i, e := range ends {
		lines[i] = s[start:e]
		start = e
	}
	return lines
}

// Drop is one spray droplet or speck of mist above the waterline, in screen cells.
type Drop struct {
	X, Row int
	Pos    float64    // where it sits in the cell, 0 at the top and 1 at the bottom
	Life   float64    // 1 when fresh, fading to 0
	Mist   bool       // a fine speck rather than a droplet
	Fg     color.RGBA // already faded toward the background by life
}

// Glyph picks a dot that sits at the drop's height in its cell.
func (d Drop) Glyph() string {
	switch {
	case d.Mist:
		return "·"
	case d.Pos < .34:
		return "˙"
	case d.Pos < .67:
		return "·"
	}
	return "."
}

// Spray returns the drops in air for a band whose top is screen row top. Rows above the band are
// all air; inside it only cells wholly above the waterline count. The slice is reused by the next call.
func (t *Water) Spray(top int) []Drop {
	if t == nil || t.still {
		return nil
	}
	r := t.ramp
	out := t.out[:0]
	for _, d := range t.drops {
		cy := math.Floor(d.y / 2)
		row, x := top+int(cy), int(d.x)
		if row < 0 || x < 0 || x >= t.w || row >= top+t.rows {
			continue
		}
		if br := row - top; br >= 0 && (t.fade[2*br*t.w+x] != 0 || t.fade[(2*br+1)*t.w+x] != 0) {
			continue
		}
		a := d.life
		if d.mist {
			a *= .55
		}
		out = append(out, Drop{X: x, Row: row, Pos: d.y/2 - cy, Life: d.life, Mist: d.mist, Fg: mix(r.key[0], r.spray, a).RGBA()})
	}
	t.out = out
	return out
}
