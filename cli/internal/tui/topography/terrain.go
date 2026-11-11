package topography

import "math"

// the same terrain as React Bits' Topography: hashed gradient noise, domain warped, then lifted into mountains

func lattice(x, y int) uint32 {
	h := uint32(x+1073741824)*2654435761 + uint32(y+1073741824)*2246822519
	h = (h ^ (h >> 15)) * 739982445
	h = (h ^ (h >> 12)) * 695872825
	return h ^ (h >> 15)
}

func corner(cx, cy int, fx, fy float64) float64 {
	h := lattice(cx, cy)
	return (float64(h&65535)/32767.5-1)*fx + (float64(h>>16)/32767.5-1)*fy
}

func noise(x, y float64) float64 {
	fx0, fy0 := math.Floor(x), math.Floor(y)
	ix, iy := int(fx0), int(fy0)
	fx, fy := x-fx0, y-fy0
	ux := fx * fx * fx * (fx*(fx*6-15) + 10)
	uy := fy * fy * fy * (fy*(fy*6-15) + 10)
	a := corner(ix, iy, fx, fy)
	b := corner(ix+1, iy, fx-1, fy)
	d := corner(ix, iy+1, fx, fy-1)
	e := corner(ix+1, iy+1, fx-1, fy-1)
	top := a + (b-a)*ux
	return top + (d+(e-d)*ux-top)*uy
}

// relief is the height of the land at tx, tz: about 0 on the plains, past 1 on the peaks
func relief(tx, tz, frequency, morphX, morphZ float64) float64 {
	px, pz := tx*frequency, tz*frequency
	wx := noise(px*.42+morphX, pz*.42+morphZ)
	wz := noise(px*.42-morphZ+5.2, pz*.42-morphX+1.3)
	px += wx * .8
	pz += wz * .8
	n := noise(px, pz) * .55
	qx, qz := 1.6*px-1.2*pz+7.1, 1.2*px+1.6*pz+3.7
	n += noise(qx, qz) * .15
	n += noise(1.6*qx-1.2*qz+1.9, 1.2*qx+1.6*qz+8.3) * .04
	s := math.Log(1+math.Exp(7*(3*n+.05))) / 7
	return s*s*1.4 + .1*n
}

func smoother(x float64) float64 {
	t := math.Max(0, math.Min(1, x))
	return t * t * t * (t*(t*6-15) + 10)
}
