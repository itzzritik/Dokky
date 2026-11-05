package ui

import (
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

const tabsX = 14

// rows: blank, mark + tabs + account, rule
func Header(w int, t float64, tabs []string, active int, account string) *lipgloss.Layer {
	p := P()
	b := NewBox()
	var mark strings.Builder
	for i, c := range "dokky" {
		mark.WriteString(Paint(string(c), RampAt(.62+.3*math.Sin(t*1.4-float64(i)*.5)), nil, true))
	}
	b.Put(3, 1, mark.String())
	x := tabsX
	rule := make([]string, w)
	for i := range rule {
		rule[i] = Paint(G.H, p.Line, nil, false)
	}
	for i, name := range tabs {
		s := Paint(name, p.Muted, nil, false)
		if i == active {
			s = Paint(name, p.Text, nil, true)
			for k := x - 1; k < x+Width(name)+1 && k < w; k++ {
				rule[k] = Paint(G.Heavy, p.Accent, nil, false)
			}
		}
		b.Hit(x, 1, "tab:"+strconv.Itoa(i), s)
		x += Width(name) + 4
	}
	if aw := Width(account); account != "" && x-4+2 <= w-3-aw {
		b.Put(w-3-aw, 1, account)
	}
	b.Put(0, 2, strings.Join(rule, ""))
	return b.Layer()
}
