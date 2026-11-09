package vault

import (
	"image/color"
	"slices"
	"strconv"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// a new master password typed twice; the only rule is its length
type newPassword struct {
	pw, again *ui.Secret
	ok        bool
}

// labels are optional: the create screen has none and lets the placeholders speak
func makeNew(label, againLabel string) newPassword {
	n := newPassword{
		pw:    ui.NewSecret(label, "At least "+strconv.Itoa(vault.MinPassword)+" characters", vault.MaxPassword),
		again: ui.NewSecret(againLabel, "Type it again", vault.MaxPassword),
	}
	n.recheck()
	return n
}

// checked on each change, not each frame, so fewer copies of the password are made
func (n *newPassword) recheck() { n.ok = vault.CheckPassword(n.pw.String()) }

func (n *newPassword) reveal(on bool) { n.pw.Reveal, n.again.Reveal = on, on }

func (n *newPassword) wipe() {
	n.pw.Wipe()
	n.again.Wipe()
	n.recheck()
}

func (n *newPassword) same() bool {
	a, b := n.pw.Runes(), n.again.Runes()
	defer clear(a)
	defer clear(b)
	return slices.Equal(a, b)
}

// the first thing wrong, as the field it is about (0 password, 1 again) and what to do; -1 when it can be used
func (n *newPassword) problem() (int, string) {
	switch {
	case n.pw.Len() == 0:
		return 0, "Type a master password."
	case !n.ok:
		return 0, "Use at least " + strconv.Itoa(vault.MinPassword) + " characters."
	case n.again.Len() == 0:
		return 1, "Type it again to confirm."
	case !n.same():
		return 1, "The two passwords don't match."
	}
	return -1, ""
}

// the two fields with a blank row between when there is room; each says how it is doing at its right end
func (n *newPassword) place(body *ui.Box, y, w, room int, ids [2]string, focus int) int {
	bg := ui.P().Surface
	g := 1
	if room >= 3 {
		g = 2
	}
	body.Add(0, y, n.pw.View(ids[0], w, focus == 0, bg))
	if m := n.count(focus == 0); m != "" {
		body.Put(w-ui.Width(m)-1, y, m)
	}
	body.Add(0, y+g, n.again.View(ids[1], w, focus == 1, bg))
	if m := n.match(focus == 1); m != "" {
		body.Put(w-ui.Width(m)-1, y+g, m)
	}
	return 1 + g
}

// a faint 5/8 while short, a tick once long enough
func (n *newPassword) count(focused bool) string {
	p := ui.P()
	l := ui.L(fieldBg(focused))
	switch k := n.pw.Len(); {
	case k == 0:
		return ""
	case n.ok:
		return l.B(ui.G.Check, p.Kelp).String()
	default:
		return l.T(strconv.Itoa(k)+"/"+strconv.Itoa(vault.MinPassword), p.Faint).String()
	}
}

// a tick once Confirm matches, a cross once it is as long and does not
func (n *newPassword) match(focused bool) string {
	p := ui.P()
	l := ui.L(fieldBg(focused))
	switch k := n.again.Len(); {
	case k == 0:
		return ""
	case n.same():
		return l.B(ui.G.Check, p.Kelp).String()
	case k >= n.pw.Len():
		return l.B(ui.G.Cross, p.Coral).String()
	}
	return ""
}

// the colour under a field's text, so a mark drawn on it blends in
func fieldBg(focused bool) color.Color {
	if focused {
		return ui.P().Sel
	}
	return ui.P().Field
}
