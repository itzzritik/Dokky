package vault

import (
	"image/color"
	"slices"
	"strconv"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const pwLimit = vault.MaxPassword + 1 // one over, so the meter can say it is too long

var strengthWords = [5]string{"Weak", "Weak", "Okay", "Good", "Strong"}

// a new master password typed twice and scored as it is typed
type newPassword struct {
	pw, again *ui.Secret
	score     int
	ok        bool
	hint      string
}

func makeNew(label string) newPassword {
	n := newPassword{pw: ui.NewSecret(label, "At least "+strconv.Itoa(vault.MinPassword)+" characters", pwLimit), again: ui.NewSecret("Confirm", "Type it again", pwLimit)}
	n.rescore()
	return n
}

// scored on each change, not each frame, so fewer copies of the password are made
func (n *newPassword) rescore() {
	n.score, n.ok, n.hint = vault.CheckPassword(n.pw.String())
}

func (n *newPassword) reveal(on bool) { n.pw.Reveal, n.again.Reveal = on, on }

func (n *newPassword) wipe() {
	n.pw.Wipe()
	n.again.Wipe()
	n.rescore()
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
		return 0, sentence(n.hint)
	case n.again.Len() == 0:
		return 1, "Type it again to confirm."
	case !n.same():
		return 1, "The two passwords don't match."
	}
	return -1, ""
}

// the password, its meter right below, then Confirm with a mark at its end; a blank row before Confirm when there is room
func (n *newPassword) place(body *ui.Box, y, w, room int, ids [2]string, focus int, bg color.Color) int {
	g := 1
	if room >= 4 {
		g = 2
	}
	body.Add(0, y, n.pw.View(ids[0], w, focus == 0, bg))
	body.Put(0, y+1, n.meter(w, bg))
	body.Add(0, y+1+g, n.again.View(ids[1], w, focus == 1, bg))
	if m := n.mark(focus == 1); m != "" {
		body.Put(w-2, y+1+g, m)
	}
	return 2 + g
}

// four bars under the field and one word flush with its right edge; empty until the first key
func (n *newPassword) meter(w int, bg color.Color) string {
	word, tone := n.status()
	bw := max(8, w-ui.LabelW-wordW-2)
	return ui.L(bg).Sp(ui.LabelW).Raw(segments(bw, n.filled(), tone, bg)).Sp(max(1, w-ui.LabelW-bw-len(word))).B(word, tone).Pad(w)
}

const wordW = 11 // the longest status word

// the meter's word: how strong, or the one thing to fix in two words
func (n *newPassword) status() (string, color.Color) {
	p := ui.P()
	tone := strengthTone(n.score)
	switch {
	case n.pw.Len() == 0:
		return "", p.Faint
	case n.ok:
		return strengthWords[n.score], tone
	case strings.HasPrefix(n.hint, "use at least"):
		return "Too short", p.Coral
	case strings.HasPrefix(n.hint, "use at most"):
		return "Too long", p.Coral
	case strings.Contains(n.hint, "spaces"):
		return "Trim spaces", p.Coral
	case strings.Contains(n.hint, "common"):
		return "Too common", p.Coral
	}
	return "Weak", p.Coral
}

// a tick once Confirm matches, a cross once it is as long and does not; drawn inside the field's right end
func (n *newPassword) mark(focused bool) string {
	p := ui.P()
	fb := p.Field
	if focused {
		fb = p.Sel
	}
	switch k := n.again.Len(); {
	case k == 0:
		return ""
	case n.same():
		return ui.L(fb).B(ui.G.Check, p.Kelp).Sp(1).String()
	case k >= n.pw.Len():
		return ui.L(fb).B(ui.G.Cross, p.Coral).Sp(1).String()
	}
	return ""
}

// one segment shows even for a very weak password, so the meter answers the first key
func (n *newPassword) filled() int {
	if n.pw.Len() == 0 {
		return 0
	}
	return max(1, n.score)
}

func strengthTone(score int) color.Color {
	p := ui.P()
	switch score {
	case 0, 1:
		return p.Coral
	case 2:
		return p.Buoy
	}
	return p.Kelp
}

// four bars with a gap between, the first n lit in tone
func segments(w, n int, tone, bg color.Color) string {
	p := ui.P()
	each := max(1, (w-3)/4)
	l := ui.L(bg)
	for i := range 4 {
		c := p.Line
		if i < n {
			c = tone
		}
		if i > 0 {
			l.Sp(1)
		}
		l.T(strings.Repeat(ui.G.Heavy, each), c)
	}
	return l.Pad(w)
}
