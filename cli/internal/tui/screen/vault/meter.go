package vault

import (
	"image/color"
	"slices"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const pwLimit = vault.MaxPassword + 1 // one over, so the meter can say it is too long

var strengthWords = [5]string{"Very weak", "Weak", "Okay", "Good", "Strong"}

// a new master password typed twice and scored as it is typed
type newPassword struct {
	pw, again *ui.Secret
	score     int
	ok        bool
	hint      string
}

func makeNew(label string) newPassword {
	n := newPassword{pw: ui.NewSecret(label, "", pwLimit), again: ui.NewSecret("Confirm", "", pwLimit)}
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

// strength meter, its hint, then the three rules with ticks; the ticks give way first when room is short
func (n *newPassword) rows(w int, bg color.Color, room int) []string {
	p := ui.P()
	tone := strengthTone(n.score)
	word, hint := "", "Use at least 10 characters."
	if n.pw.Len() > 0 {
		word, hint = strengthWords[n.score], sentence(n.hint)
	}
	// "good" and "strong" only repeat the meter's word
	if strings.EqualFold(strings.TrimSuffix(hint, "."), word) {
		hint = ""
	}
	bw := max(8, w-ui.LabelW-len(strengthWords[0])-2)
	meter := ui.L(bg).T("Strength", p.Muted).Pad(ui.LabelW) + ui.L(bg).Raw(segments(bw, n.filled(), tone, bg)).Sp(2).B(word, tone).Pad(w-ui.LabelW)
	hc := p.Muted
	if n.pw.Len() > 0 && !n.ok {
		hc = tone
	}
	out := []string{meter, ui.L(bg).Sp(ui.LabelW).T(hint, hc).Pad(w), n.checks(w, bg)}
	return out[:max(0, min(len(out), room))]
}

// a blank row between fields when the window has room for every row, none when it is tight
func gap(room, fields, after int) int {
	if 2*fields+after <= room {
		return 2
	}
	return 1
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

// the rules as ticks, shorter words when the row is narrow
func (n *newPassword) checks(w int, bg color.Color) string {
	p := ui.P()
	k := n.pw.Len()
	met := []bool{k >= vault.MinPassword && k <= vault.MaxPassword, n.ok, n.again.Len() > 0 && n.same()}
	for _, set := range [][]string{{"10 or more characters", "Hard to guess", "Both match"}, {"10+ characters", "Strong enough", "Match"}} {
		l := ui.L(bg)
		for i, label := range set {
			if i > 0 {
				l.Sp(3)
			}
			if met[i] {
				l.B(ui.G.Check, p.Kelp).Sp(1).T(label, p.Text)
			} else {
				l.T(ui.G.Ring, p.Faint).Sp(1).T(label, p.Muted)
			}
		}
		if l.Width() <= w {
			return l.Pad(w)
		}
	}
	return ui.L(bg).Pad(w)
}
