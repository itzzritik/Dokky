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

// the Show toggle on the first field shows or hides both
func (n *newPassword) flip() { n.pw.Reveal = !n.pw.Reveal; n.again.Reveal = n.pw.Reveal }

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
// labelled puts each field under its label, as Forged's create card does; at is the focused stop, "" while busy;
// returns the rows used
func (n *newPassword) labelled(body *ui.Box, y, w int, labels, ids [2]string, at string) int {
	p := ui.P()
	bg := p.Window
	for i, f := range []*ui.Secret{n.pw, n.again} {
		body.Put(0, y+3*i, ui.L(bg).T(labels[i], p.Muted).Pad(w))
		body.Add(0, y+3*i+1, f.View(ids[i], w, at, bg))
		n.mark(body, i, w, y+3*i+1, at == ids[i])
	}
	return 5
}

// how the field is doing, at its right end left of a Show toggle
func (n *newPassword) mark(body *ui.Box, i, w, y int, focused bool) {
	m, f := n.count(focused), n.pw
	if i == 1 {
		m, f = n.match(focused), n.again
	}
	if m != "" {
		body.Put(w-f.EyeW()-ui.Width(m)-1, y, m)
	}
}

func (n *newPassword) place(body *ui.Box, y, w, room int, ids [2]string, at string) int {
	bg := ui.P().Window
	g := 1
	if room >= 3 {
		g = 2
	}
	body.Add(0, y, n.pw.View(ids[0], w, at, bg))
	n.mark(body, 0, w, y, at == ids[0])
	body.Add(0, y+g, n.again.View(ids[1], w, at, bg))
	n.mark(body, 1, w, y+g, at == ids[1])
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
