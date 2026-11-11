package contour

import "math"

// perm is Ken Perlin's improved noise table, shuffled from a fixed seed so every run draws the same terrain
var perm = func() [512]uint8 {
	var p [256]int
	for i := range p {
		p[i] = i
	}
	s := int64(7)
	for i := 255; i > 0; i-- {
		s = s * 16807 % 2147483647
		j := int(s % int64(i+1))
		p[i], p[j] = p[j], p[i]
	}
	var out [512]uint8
	for i := range out {
		out[i] = uint8(p[i&255])
	}
	return out
}()

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func lerp(a, b, t float64) float64 { return a + t*(b-a) }

func grad(h uint8, x, y, z float64) float64 {
	h &= 15
	u, v := y, z
	if h < 8 {
		u = x
	}
	switch {
	case h < 4:
		v = y
	case h == 12 || h == 14:
		v = x
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

// noise is 3D gradient noise in about -1 to 1
func noise(x, y, z float64) float64 {
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	X, Y, Z := int(fx)&255, int(fy)&255, int(fz)&255
	x, y, z = x-fx, y-fy, z-fz
	u, v, w := fade(x), fade(y), fade(z)
	A := int(perm[X]) + Y
	AA, AB := int(perm[A])+Z, int(perm[A+1])+Z
	B := int(perm[X+1]) + Y
	BA, BB := int(perm[B])+Z, int(perm[B+1])+Z
	return lerp(lerp(lerp(grad(perm[AA], x, y, z), grad(perm[BA], x-1, y, z), u),
		lerp(grad(perm[AB], x, y-1, z), grad(perm[BB], x-1, y-1, z), u), v),
		lerp(lerp(grad(perm[AA+1], x, y, z-1), grad(perm[BA+1], x-1, y, z-1), u),
			lerp(grad(perm[AB+1], x, y-1, z-1), grad(perm[BB+1], x-1, y-1, z-1), u), v), w)
}

// fbm layers three octaves of noise, the soft rolling hills of the map
func fbm(x, y, z float64) float64 {
	s, a, f := 0.0, .5, 1.0
	for range 3 {
		s += a * noise(x*f, y*f, z*f)
		f *= 2
		a *= .5
	}
	return s
}
