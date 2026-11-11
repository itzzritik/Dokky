package ui

import (
	"image/color"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// a typed secret kept as runes so it can be wiped; typing only appends, like a password prompt
type Secret struct {
	Label, Hint string
	Reveal      bool
	Eye         bool                // a Show or Hide toggle at the strip's right end, EyeID of the field
	Show        func([]rune) string // how a revealed value reads, like a recovery key in groups
	buf         []rune
	limit       int
}

// the buffer never grows past limit, so no copy of the secret is left behind in an old array
func NewSecret(label, hint string, limit int) *Secret {
	return &Secret{Label: label, Hint: hint, buf: make([]rune, 0, limit), limit: limit}
}

func (s *Secret) add(r rune) {
	if !unicode.IsControl(r) && len(s.buf) < s.limit {
		s.buf = append(s.buf, r)
	}
}

// reports whether msg changed the value
func (s *Secret) Type(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.PasteMsg:
		n := len(s.buf)
		for _, r := range m.Content {
			s.add(r)
		}
		return len(s.buf) != n
	case tea.KeyPressMsg:
		switch m.String() {
		case "backspace", "ctrl+h":
			n := len(s.buf)
			if n == 0 {
				return false
			}
			s.buf[n-1] = 0
			s.buf = s.buf[:n-1]
			return true
		case "ctrl+u":
			n := len(s.buf)
			s.Wipe()
			return n > 0
		}
		if m.Text != "" {
			n := len(s.buf)
			for _, r := range m.Text {
				s.add(r)
			}
			return len(s.buf) != n
		}
	}
	return false
}

func (s *Secret) Len() int { return len(s.buf) }

func (s *Secret) Wipe() {
	clear(s.buf)
	s.buf = s.buf[:0]
}

// a copy the caller wipes
func (s *Secret) Runes() []rune { return slices.Clone(s.buf) }

// the value as the daemon takes it; strings can't be wiped, so only the call holds one
func (s *Secret) String() string { return string(s.buf) }

// EyeID is the Show or Hide toggle at the right end of field id's strip
func EyeID(id string) string { return "eye:" + id }

func IsEye(id string) bool { return strings.HasPrefix(id, "eye:") }

// how wide the toggle is, so a mark on the strip sits left of it
func (s *Secret) EyeW() int {
	if s.Eye {
		return 6
	}
	return 0
}

// label on the window fill, value on its own strip, masked unless revealed; the newest characters stay in view.
// at is the focused stop: the field, its toggle, or "" while nothing here takes keys
func (s *Secret) View(id string, w int, at string, bg color.Color) *lipgloss.Layer {
	p := P()
	focused := at == id
	fb, bar := p.Field, " "
	if focused {
		fb, bar = p.Sel, Paint(G.Bar, p.Accent, p.Sel, false)
	}
	lw := LabelW
	if s.Label == "" {
		lw = 0
	}
	ew := s.EyeW()
	iw := max(1, w-lw-2-ew)
	var val []rune
	switch {
	case s.Reveal && s.Show != nil:
		val = []rune(s.Show(s.buf))
	case s.Reveal:
		val = s.buf
	default:
		val = []rune(strings.Repeat(G.Mask, len(s.buf)))
	}
	in := L(fb).Raw(bar).Sp(1)
	switch {
	case len(val) == 0 && !focused:
		in.T(s.Hint, p.Faint)
	case len(val) == 0:
		hint := []rune(s.Hint + " ")
		caretFg = p.Faint
		in.Raw(Paint(string(hint[0]), caretInk, fb, false)).T(string(hint[1:]), p.Faint)
	case focused:
		in.T(string(val[max(0, len(val)-iw+1):]), p.Text)
		caretFg = p.Text
		in.Raw(Paint(" ", caretInk, fb, false))
	default:
		in.T(string(val[max(0, len(val)-iw):]), p.Text)
	}
	b := NewBox().Hit(0, 0, id, L(bg).T(s.Label, p.Muted).Pad(lw)+in.Pad(w-lw-ew))
	if s.Eye {
		word := "Show"
		if s.Reveal {
			word = "Hide"
		}
		eye := L(fb).Sp(1).T(word, p.Muted).Sp(1).String()
		if at == EyeID(id) {
			eye = L(p.Sel).Raw(Paint(G.Bar, p.Accent, p.Sel, false)).B(word, p.Accent).Sp(1).String()
		}
		b.Hit(w-ew, 0, EyeID(id), eye)
	}
	return b.Layer()
}
