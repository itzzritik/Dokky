package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const compactX = 10

// rows: blank, mark + tabs + chip, rule; tabs sit centred between the mark and the chip, compact ("‹ Overview ›  1/5") when crowded
func Header(w int, tabs []string, active int, chip string) *lipgloss.Layer {
	p := P()
	b := NewBox()
	brand := Paint("dokky", p.Accent, nil, true)
	b.Put(2, 1, brand)
	rule := make([]string, w)
	for i := range rule {
		rule[i] = " "
		if i >= 2 && i < w-2 {
			rule[i] = Paint(G.H, p.Cap, nil, false)
		}
	}
	// the active tab's underline sweeps along the water ramp
	under := func(from, n int) {
		for i := 0; i < n; i++ {
			if k := from + i; k >= 0 && k < w {
				f := .5
				if n > 1 {
					f = float64(i) / float64(n-1)
				}
				rule[k] = Paint(G.Heavy, RampAt(.42+.38*f), nil, false)
			}
		}
	}
	gap := 2
	if w >= 90 {
		gap = 4
	}
	full := gap * (len(tabs) - 1)
	for _, name := range tabs {
		full += Width(name)
	}
	chipX := w - 2 - Width(chip)
	x := max(2+Width(brand)+2, min((w-full)/2, chipX-2-full))
	if x+full <= chipX-2 {
		for i, name := range tabs {
			s := Paint(name, p.Muted, nil, false)
			if i == active {
				s = Paint(name, p.Text, nil, true)
				under(x-1, Width(name)+2)
			}
			b.Hit(x, 1, "tab:"+strconv.Itoa(i), s)
			x += Width(name) + gap
		}
		x -= gap
	} else {
		x = compactX
		arrow := func(g string, to int) {
			if to < 0 || to >= len(tabs) {
				b.Put(x, 1, Paint(g, p.Line, nil, false))
			} else {
				b.Hit(x, 1, "tab:"+strconv.Itoa(to), Paint(g, p.Muted, nil, false))
			}
			x += Width(g) + 1
		}
		name := tabs[active]
		arrow(G.Back, active-1)
		b.Put(x, 1, Paint(name, p.Text, nil, true))
		under(x-1, Width(name)+2)
		x += Width(name) + 1
		arrow(G.Arrow, active+1)
		pos := strconv.Itoa(active+1) + "/" + strconv.Itoa(len(tabs))
		b.Put(x+1, 1, Paint(pos, p.Faint, nil, false))
		x += 1 + Width(pos)
	}
	if chip != "" && x+2 <= chipX {
		b.Put(chipX, 1, chip)
	}
	b.Put(0, 2, strings.Join(rule, ""))
	return b.Layer()
}
