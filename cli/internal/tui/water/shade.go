package water

import "math"

// shade turns the sim into a brightness and a coverage per half block pixel
func (t *Water) shade() {
	w, P := t.w, float64(t.px)
	lb := .34 * (P - t.rest)
	ck := t.clock
	lace := int(math.Floor(t.drift * 2))
	for x := 0; x < w; x++ {
		s0, s1 := t.surf[2*x], t.surf[2*x+1]
		sm := (s0 + s1) / 2
		f := (t.foam[2*x] + t.foam[2*x+1]) / 2
		fd := .6 + 2.4*f
		sk := t.sheen[x*sheens : (x+1)*sheens]
		for py := 0; py < t.px; py++ {
			i := py*w + x
			c := (clamp(float64(py+1)-s0, 0, 1) + clamp(float64(py+1)-s1, 0, 1)) / 2
			t.cov[i] = c
			if c <= 0 {
				t.val[i] = 0
				continue
			}
			y := float64(py) + .5
			d := max(0, y-sm)
			// water swallows light with depth, so it darkens away from the surface
			v := .03 + .55*math.Exp(-d/lb) - .04*y/P
			// rays add light, the gaps between them only dim a little
			if l := min(2.2, t.lightAt(x, d)) - 1; l > 0 {
				v += .38 * l * smooth(.8, 3.5, d) * math.Exp(-d/15)
			} else {
				v += .12 * l * smooth(.8, 3.5, d)
			}
			// two drifting layers of the network meet only in places, so the mesh shimmers and reforms
			if cw := smooth(.3, 1.8, d) * math.Exp(-d/4.5); cw > .02 {
				u := float64(x)*.45 + 3*t.slope[2*x]
				n := (t.netAt(u+ck*1.8, d*.8+ck*.6) + t.netAt(u*1.13-ck*1.4+17, d*.9-ck*.4+31)) / 2
				v += .65 * n * n * cw
			}
			if d < sheenD {
				j := d / sheenD * (sheens - 1)
				k := min(sheens-2, int(j))
				s := sk[k] + (sk[k+1]-sk[k])*(j-float64(k))
				a := 1 - smooth(0, sheenD, d)
				v += (s - v) * a
			}
			if f > .02 && d < fd {
				m := t.lace[(int(d*2)%8)*t.sw+((2*x-lace)%t.sw+t.sw)%t.sw]
				a := smooth(m-.12, m+.12, min(1, 2.2*f)*(1.25-d/fd))
				v += (1 - v) * a * .95 * c * c
			}
			t.val[i] = v
		}
	}
	for _, b := range t.bubbles {
		x := int(b.x)
		if x < 0 || x >= w {
			continue
		}
		y := b.y - .5
		k := int(math.Floor(y))
		fy := y - float64(k)
		for n, wt := range [2]float64{1 - fy, fy} {
			py := k + n
			if py < 0 || py >= t.px {
				continue
			}
			i := py*w + x
			if t.cov[i] > .5 {
				t.val[i] += (.82 - t.val[i]) * wt * b.a
			}
		}
	}
	for i, v := range t.val {
		t.lvl[i] = uint8(clamp(v, 0, 1)*(levels-1) + .5)
		y := float64(i/w) + .5
		t.fade[i] = uint8(smooth(.3, .7, t.cov[i])*smooth(0, 3, y)*(fades-1) + .5)
	}
}

// caustic density at depth d below the surface, 1 is plain light
func (t *Water) lightAt(x int, d float64) float64 {
	if d <= causticAt[0] {
		return t.light[x]
	}
	for k := 1; k < depths; k++ {
		if d <= causticAt[k] {
			f := (d - causticAt[k-1]) / (causticAt[k] - causticAt[k-1])
			a, b := t.light[(k-1)*t.w+x], t.light[k*t.w+x]
			return a + (b-a)*f
		}
	}
	return t.light[(depths-1)*t.w+x]
}
