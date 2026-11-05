package ui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const LabelW = 14

type Field struct {
	Label, Hint string
	Secret      bool
	Bare        bool // no strip or bar, for a field inside a selected card
	Input       textinput.Model
}

// a focused field inks its caret cell with this; Compose finds it, gives the cell caretFg back and reports where the terminal's own blinking bar goes
var (
	caretInk = color.RGBA{1, 2, 3, 255}
	caretFg  color.Color
)

func NewField(label, value, hint string, secret bool) *Field {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 4096
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return &Field{Label: label, Hint: hint, Secret: secret, Input: in}
}

func (f *Field) Value() string { return f.Input.Value() }

// only typing reaches the input; its cursor blink is never drawn, so blink messages stay out
func (f *Field) Type(msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg:
		var cmd tea.Cmd
		f.Input, cmd = f.Input.Update(msg)
		return cmd, true
	}
	return nil, false
}

// label on the window fill, value on its own strip; the caret is the terminal's cursor
func (f *Field) View(id string, w int, focused bool, bg color.Color) *lipgloss.Layer {
	p := P()
	fb, bar := p.Field, " "
	if focused {
		fb, bar = p.Sel, Paint(G.Bar, p.Accent, p.Sel, false)
	}
	lw := LabelW
	if f.Label == "" {
		lw = 0
	}
	iw := w - lw - 2
	val := []rune(f.Input.Value())
	if f.Secret {
		val = []rune(strings.Repeat(G.Mask, len(val)))
	}
	pos := min(f.Input.Position(), len(val))
	start := max(0, pos-iw+1)
	in := L(fb).Raw(bar).Sp(1)
	if f.Bare {
		fb, in = bg, L(bg)
	}
	switch {
	case len(val) == 0 && !focused:
		in.T(f.Hint, p.Faint)
	case len(val) == 0:
		hint := []rune(f.Hint + " ")
		caretFg = p.Faint
		in.Raw(Paint(string(hint[0]), caretInk, fb, false)).T(string(hint[1:]), p.Faint)
	case focused:
		end := min(len(val), start+iw)
		in.T(string(val[start:pos]), p.Text)
		caretFg = p.Text
		if pos < len(val) {
			in.Raw(Paint(string(val[pos]), caretInk, fb, false)).T(string(val[pos+1:end]), p.Text)
		} else {
			in.Raw(Paint(" ", caretInk, fb, false))
		}
	default:
		in.T(string(val[:min(len(val), iw)]), p.Text)
	}
	line := L(bg).T(f.Label, p.Muted).Pad(lw) + in.Pad(w-lw)
	return lipgloss.NewLayer(line).ID(id)
}

func Chips(id string, labels []string, sel int, focused bool, bg color.Color) *lipgloss.Layer {
	p := P()
	b := NewBox()
	x := 0
	for i, label := range labels {
		fg, cb := p.Muted, Mix(p.Surface, p.Text, .08)
		if i == sel {
			fg, cb = p.OnAccent, Mix(p.Surface, p.Accent, .6)
			if focused {
				cb = p.Accent
			}
		}
		s := Paint("  "+label+"  ", fg, cb, i == sel)
		b.Hit(x, 0, id+":"+string(rune('0'+i)), s)
		x += Width(s)
		if i < len(labels)-1 {
			b.Put(x, 0, Paint(" ", nil, bg, false))
			x++
		}
	}
	return b.Layer()
}

// the focused button is filled, coral when it destroys something; the rest sit on the cap colour
func Button(id, label string, primary, danger, focused bool) *lipgloss.Layer {
	p := P()
	fg, bg := p.Text, p.Cap
	switch {
	case focused && danger:
		fg, bg = p.OnAccent, p.Coral
	case focused:
		fg, bg = p.OnAccent, p.Accent
	case danger:
		fg = p.Coral
	}
	return lipgloss.NewLayer(Paint("  "+label+"  ", fg, bg, focused)).ID(id)
}

func ButtonWidth(label string) int { return Width(label) + 4 }
