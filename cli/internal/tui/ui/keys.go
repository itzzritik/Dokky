package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func Keycap(k string) string { return Paint(" "+k+" ", P().Text, P().Cap, true) }

func hint(a core.Action, label bool) string {
	key, fg := Keycap(a.Key), P().Muted
	switch {
	case a.Accent:
		key, fg = Paint(" "+a.Key+" ", P().OnAccent, P().Accent, true), P().Accent
	case a.Danger:
		fg = P().Coral
	}
	if !label {
		return key
	}
	return key + " " + Paint(Capital(a.Label), fg, nil, false)
}

func HintsWidth(acts []core.Action, labels bool) int {
	w, gap := 0, 2
	if labels {
		gap = 3
	}
	for i, a := range acts {
		if i > 0 {
			w += gap
		}
		w += Width(hint(a, labels))
	}
	return w
}

// Forged's footer: up to 4 screen keys left, More, Update and Quit right. short on room, the right side keeps
// More and an accent key, then the last screen keys go, then the labels
func Footer(w int, left, right []core.Action) *lipgloss.Layer {
	avail := w - 6
	more := right
	if len(more) > 1 {
		more = []core.Action{right[0]}
		for _, a := range right[1:] {
			if a.Accent {
				more = append(more, a)
			}
		}
	}
	first := func(n int) []core.Action { return left[:min(n, len(left))] }
	cands := []struct {
		l, r  []core.Action
		label bool
	}{
		{left, right, true}, {left, more, true}, {first(3), more, true}, {first(2), more, true},
		{first(1), more, true}, {first(3), more, false}, {first(2), more, false}, {nil, more, true}, {nil, more, false},
	}
	b := NewBox()
	put := func(x int, acts []core.Action, label bool) {
		gap := 2
		if label {
			gap = 3
		}
		for _, a := range acts {
			h := hint(a, label)
			// a hint for several keys, like up and down, is no one key to press
			if core.Press(a.Key).Code == 0 {
				b.Put(x, 0, h)
			} else {
				b.Hit(x, 0, "key:"+a.Key, h)
			}
			x += Width(h) + gap
		}
	}
	for _, c := range cands {
		lw, rw := HintsWidth(c.l, c.label), HintsWidth(c.r, c.label)
		used := lw + rw
		if lw > 0 && rw > 0 {
			used += 3
		}
		if used > avail {
			continue
		}
		put(3, c.l, c.label)
		put(w-3-rw, c.r, c.label)
		break
	}
	return b.Layer()
}
