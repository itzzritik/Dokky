package ui

import (
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const tabsX = 14

// rows: blank, mark + tabs + chip, rule; tabs go compact ("‹ Overview ›  1/5") when narrow or crowded
func Header(w int, t float64, tabs []string, active int, chip string) *lipgloss.Layer {
	p := P()
	b := NewBox()
	var mark strings.Builder
	for i, c := range "dokky" {
		mark.WriteString(Paint(string(c), RampAt(.62+.3*math.Sin(t*1.4-float64(i)*.5)), nil, true))
	}
	b.Put(3, 1, mark.String())
	rule := make([]string, w)
	for i := range rule {
		rule[i] = Paint(G.H, p.Line, nil, false)
	}
	under := func(from, n int) {
		for k := max(0, from); k < from+n && k < w; k++ {
			rule[k] = Paint(G.Heavy, p.Accent, nil, false)
		}
	}
	gap := 4
	if w >= 120 {
		gap = 6
	}
	full := gap * (len(tabs) - 1)
	for _, name := range tabs {
		full += Width(name)
	}
	chipX := w - 3 - Width(chip)
	x := tabsX
	if w >= 90 && x+full+2 <= chipX {
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
