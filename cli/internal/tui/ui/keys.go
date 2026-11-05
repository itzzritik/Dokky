package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func Keycap(k string) string { return Paint(" "+k+" ", P().Text, P().Cap, true) }

func hint(a core.Action) string {
	label := P().Muted
	if a.Danger {
		label = P().Coral
	}
	return Keycap(a.Key) + " " + Paint(a.Label, label, nil, false)
}

// hints that do not fit are left out from the middle, keeping the last one (quit)
func Footer(w int, acts []core.Action, version string) *lipgloss.Layer {
	b := NewBox()
	right := w - 3 - Width(version)
	b.Put(right, 0, Paint(version, P().Faint, nil, false))
	room := right - 2 - 3
	keep := acts
	for len(keep) > 1 {
		n := 0
		for _, a := range keep {
			n += Width(hint(a)) + 3
		}
		if n <= room {
			break
		}
		keep = append(keep[:len(keep)-2:len(keep)-2], keep[len(keep)-1])
	}
	x := 3
	for _, a := range keep {
		h := hint(a)
		if x+Width(h) > right-2 {
			break
		}
		b.Hit(x, 0, "key:"+a.Key, h)
		x += Width(h) + 3
	}
	return b.Layer()
}
