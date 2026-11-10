package water

import (
	"math"
	"math/rand/v2"
)

const tile = 64 // caustic network tile, a power of two so it wraps with a mask

// network bakes a tiling voronoi edge map: under a rippled surface light gathers into a mesh of bright lines
func network(rng *rand.Rand) []float32 {
	const n = 18
	var px, py [n]float64
	for i := range n {
		px[i], py[i] = rng.Float64()*tile, rng.Float64()*tile
	}
	g := make([]float32, tile*tile)
	for y := range tile {
		for x := range tile {
			f1, f2 := math.Inf(1), math.Inf(1)
			for i := range n {
				dx := math.Abs(float64(x) + .5 - px[i])
				dy := math.Abs(float64(y) + .5 - py[i])
				dx, dy = min(dx, tile-dx), min(dy, tile-dy)
				d := math.Sqrt(dx*dx + dy*dy)
				if d < f1 {
					f1, f2 = d, f1
				} else if d < f2 {
					f2 = d
				}
			}
			e := (f2 - f1) / 2.2
			g[y*tile+x] = float32(math.Exp(-e * e))
		}
	}
	return g
}

func (t *Water) netAt(u, v float64) float64 {
	fu, fv := math.Floor(u), math.Floor(v)
	i, j := int(fu)&(tile-1), int(fv)&(tile-1)
	a, b := u-fu, v-fv
	i1, j1 := (i+1)&(tile-1), (j+1)&(tile-1)
	n := t.net
	top := float64(n[j*tile+i])*(1-a) + float64(n[j*tile+i1])*a
	bot := float64(n[j1*tile+i])*(1-a) + float64(n[j1*tile+i1])*a
	return top*(1-b) + bot*b
}

// sunlight bends at the sloped surface and bunches up below troughs; gathering where it lands gives moving light shafts
func (t *Water) caustics() {
	const slant, bend = .45, 2.6
	w := t.w
	for d := range depths {
		D := causticAt[d]
		row := t.light[d*w : (d+1)*w]
		clear(row)
		for i := 0; i < t.sw; i++ {
			x := (float64(i)+.5)/2 + D*(slant+bend*t.slope[i])
			x -= math.Floor(x/float64(w)) * float64(w)
			k := int(x)
			f := x - float64(k)
			row[k%w] += .5 * (1 - f)
			row[(k+1)%w] += .5 * f
		}
		for range 2 + 2*d {
			prev, first := row[w-1], row[0]
			for x := 0; x < w; x++ {
				next := first
				if x+1 < w {
					next = row[x+1]
				}
				cur := row[x]
				row[x] = (prev + 2*cur + next) / 4
				prev = cur
			}
		}
	}
}
