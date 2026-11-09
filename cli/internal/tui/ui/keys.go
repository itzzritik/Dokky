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
	return key + " " + Paint(a.Label, fg, nil, false)
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

// screen keys left, globals right; short on room, globals fold into more, the last screen keys go (more has them), then labels, then keys
func Footer(w int, left, right []core.Action, more *core.Action, version string) *lipgloss.Layer {
	avail := w - 6
	folded := right
	if more != nil {
		folded = []core.Action{*more}
		for _, a := range right {
			if a.Accent {
				folded = append(folded, a)
			}
		}
	}
	type cand struct {
		l, r   []core.Action
		ll, rl bool
	}
	cands := []cand{{left, right, true, true}, {left, folded, true, true}}
	// the last screen keys go before any label does, and more says where they went
	if more != nil {
		for n := len(left) - 1; n >= 2; n-- {
			cands = append(cands, cand{left[:n], folded, true, true})
		}
	}
	cands = append(cands, cand{left, folded, true, false})
	for n := len(left); n >= 0; n-- {
		cands = append(cands, cand{left[:n], folded, false, false})
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
		lw, rw := HintsWidth(c.l, c.ll), HintsWidth(c.r, c.rl)
		used := lw + rw
		if lw > 0 && rw > 0 {
			used += 3
		}
		if used > avail {
			continue
		}
		rx := w - 3 - rw
		if vw := Width(version); c.ll && c.rl && used+3+vw <= avail {
			b.Put(w-3-vw, 0, Paint(version, P().Faint, nil, false))
			rx -= vw + 3
		}
		put(3, c.l, c.ll)
		put(rx, c.r, c.rl)
		break
	}
	return b.Layer()
}
