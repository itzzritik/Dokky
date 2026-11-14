package ui

import (
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func Keycap(k string) string { return Paint(" "+k+" ", P().Text, P().Cap, true) }

func hint(a core.Action, label bool) string { return hintOn(a, label, nil) }

// a hint whose label sits on bg, like inside a card
func hintOn(a core.Action, label bool, bg color.Color) string {
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
	return key + Paint(" "+Capital(a.Label), fg, bg, false)
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

// Forged's footer: up to 4 screen keys left, More, Update and Quit right, and tail, faint, in the corner.
// short on room, the tail goes first, then the right side keeps More and an accent key, then the last screen
// keys go, then the labels
func Footer(w int, left, right []core.Action, tail string) *lipgloss.Layer {
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
		rx := w - 3 - rw
		// the tail only takes room no key needs
		if tw := Width(tail); tw > 0 && c.label && used+3+tw <= avail {
			b.Put(w-3-tw, 0, Paint(tail, P().Faint, nil, false))
			rx -= tw + 3
		}
		put(3, c.l, c.label)
		put(rx, c.r, c.label)
		break
	}
	return b.Layer()
}

// WindowKeys is what a window's own footer shows: the keys that do something its buttons don't say, and esc.
// Arrows only move focus, and enter presses the lit button, which names what it does
func WindowKeys(acts []core.Action) []core.Action {
	return slices.DeleteFunc(slices.Clone(acts), func(a core.Action) bool {
		return a.Key == "enter" || strings.Trim(a.Key, "↑↓←→ ") == ""
	})
}
