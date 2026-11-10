package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

// content sits inside the border and 2 cells of padding
const WindowPad = 3

// a button in a window's action bar
type Btn struct {
	ID, Label       string
	Primary, Danger bool
}

type StepState int

const (
	StepNext StepState = iota
	StepNow
	StepDone
	StepSkipped
)

// one stop in a window's stepper
type Step struct {
	Label string
	State StepState
}

// every modal: a surface with a rounded border, the title in its top edge, a stepper, text, body, buttons at the bottom right
type Window struct {
	W, H            int
	Title, Sub      string
	Steps           []Step
	Corner          string // painted text in the top border's right end, like a status
	Ok, Bad, SubBad bool
	Body            *Box
	Buttons         []Btn
	Active          int
	Flush           bool          // the body spans border to border with no padding, for rows that draw their own bar
	Hints           []core.Action // keys drawn left of the buttons, Forged's in-card footer
}

func (w *Window) Inner() int { return w.W - 2*WindowPad }

func (w *Window) top() int {
	if w.Flush {
		return 1
	}
	t := 2
	if len(w.Steps) > 0 {
		t += 2
	}
	if w.Sub != "" {
		t += len(w.subLines()) + 1
	}
	return t
}

// the sub line wraps to two lines at most, the second cut short, as Forged's prompts do
func (w *Window) subLines() []string {
	lines := Wrap(w.Sub, w.Inner())
	if len(lines) > 2 {
		lines = []string{lines[0], core.Fit(strings.Join(lines[1:], " "), w.Inner())}
	}
	return lines
}

func (w *Window) bottom() int {
	switch {
	case len(w.Buttons) > 0 || len(w.Hints) > 0:
		return 3
	case w.Flush:
		return 1
	}
	return 2
}

// sizes the window to its body rows, never taller than maxH; with no body the text's gap is the only one
func (w *Window) Fit(rows, maxH int) {
	h := w.top() + rows + w.bottom()
	if rows == 0 && w.top() > 2 {
		h--
	}
	w.H = min(maxH, h)
}

// body rows that fit in maxH
func (w *Window) Room(maxH int) int { return max(1, maxH-w.top()-w.bottom()) }

// Edge is the width a Flush body gets
func (w *Window) Edge() int { return w.W - 2 }

// a rounded border on the window colour, its title in the top edge; nothing is drawn outside the border
func (w *Window) Layer() *lipgloss.Layer {
	p := P()
	bg := p.Window
	cw := w.Inner()
	line := func(s string) string { return Paint(s, p.Line, bg, false) }
	blank := func(n int) string { return Paint(strings.Repeat(" ", n), nil, bg, false) }

	tc := p.Text
	title := L(bg)
	switch {
	case w.Ok:
		title.B(G.Check+" ", p.Kelp)
	case w.Bad:
		tc = p.Coral
	}
	title.B(core.Fit(w.Title, w.W-10), tc)
	tl := L(bg)
	if w.Title != "" {
		tl.Sp(1).Raw(title.String()).Sp(1)
	}
	rl := L(bg)
	if w.Corner != "" {
		rl.Sp(1).Raw(w.Corner).Sp(1)
	}
	run := max(0, w.W-4-tl.Width()-rl.Width())
	fill := make([]string, w.H)
	for i := range fill {
		switch i {
		case 0:
			fill[i] = line(G.TL+G.H) + tl.String() + line(strings.Repeat(G.H, run)) + rl.String() + line(G.H+G.TR)
		case w.H - 1:
			fill[i] = line(G.BL + strings.Repeat(G.H, w.W-2) + G.BR)
		default:
			fill[i] = line(G.V) + blank(w.W-2) + line(G.V)
		}
	}
	root := lipgloss.NewLayer(strings.Join(fill, "\n")).Z(-1)

	b := NewBox()
	y := 2
	if len(w.Steps) > 0 {
		b.Put(WindowPad, y, stepper(w.Steps, cw, bg))
		y += 2
	}
	if w.Sub != "" {
		sc := p.Muted
		if w.SubBad {
			sc = p.Coral
		}
		for i, l := range w.subLines() {
			b.Put(WindowPad, y+i, L(bg).T(l, sc).Pad(cw))
		}
	}

	box := lipgloss.NewLayer("").AddLayers(root, b.Layer())
	if w.Body != nil {
		x := WindowPad
		if w.Flush {
			x = 1
		}
		box.AddLayers(w.Body.Layer().X(x).Y(w.top()))
	}
	bw := 0
	if len(w.Buttons) > 0 {
		bar := NewBox()
		x := 0
		for i, btn := range w.Buttons {
			bar.Add(x, 0, Button("btn:"+btn.ID, btn.Label, btn.Primary, btn.Danger, i == w.Active))
			x += ButtonWidth(btn.Label) + 2
		}
		bw = x - 2
		box.AddLayers(bar.Layer().X(WindowPad + cw - bw).Y(w.H - 2))
	}
	if len(w.Hints) > 0 {
		box.AddLayers(cardHints(w.Hints, cw-bw-2, bg).X(WindowPad).Y(w.H - 2))
	}
	return box
}

// the in-card hints: all of them, else the last, labelled while there is room
func cardHints(acts []core.Action, room int, bg color.Color) *lipgloss.Layer {
	b := NewBox()
	last := acts[len(acts)-1:]
	for _, c := range []struct {
		acts  []core.Action
		label bool
	}{{acts, true}, {last, true}, {acts, false}, {last, false}} {
		if HintsWidth(c.acts, c.label) > room {
			continue
		}
		x, gap := 0, 2
		if c.label {
			gap = 3
		}
		for _, a := range c.acts {
			h := hintOn(a, c.label, bg)
			if core.Press(a.Key).Code == 0 {
				b.Put(x, 0, h)
			} else {
				b.Hit(x, 0, "key:"+a.Key, h)
			}
			x += Width(h) + gap
		}
		break
	}
	return b.Layer()
}

// a text row inside a window
func (w *Window) Text(s string, fg color.Color) string { return L(P().Window).T(s, fg).Pad(w.Inner()) }

// which button a click hit, or -1
func (w *Window) Index(id string) int {
	for i, b := range w.Buttons {
		if "btn:"+b.ID == id {
			return i
		}
	}
	return -1
}

// the steps in a row joined by lines that fill the width; the walked part of the line is teal, and only the current label stays when the row is tight
func stepper(steps []Step, w int, bg color.Color) string {
	p := P()
	mark := func(s Step) (string, color.Color, color.Color) {
		switch s.State {
		case StepDone:
			return G.Check, p.Kelp, p.Muted
		case StepNow:
			return G.Here, p.Accent, p.Text
		case StepSkipped:
			return G.Dash, p.Faint, p.Faint
		}
		return G.Ring, p.Faint, p.Faint
	}
	gaps := max(1, len(steps)-1)
	label := func(s Step, all bool) bool { return all || s.State == StepNow }
	width := func(all bool) int {
		n := 0
		for _, s := range steps {
			n++
			if label(s, all) {
				n += 1 + Width(s.Label)
			}
		}
		return n
	}
	all := w-width(true) >= gaps*5
	free := max(gaps*3, w-width(all))
	l := L(bg)
	for i, s := range steps {
		g, gc, tc := mark(s)
		l.B(g, gc)
		if label(s, all) {
			if s.State == StepNow {
				l.Sp(1).B(s.Label, tc)
			} else {
				l.Sp(1).T(s.Label, tc)
			}
		}
		if i == len(steps)-1 {
			break
		}
		n := free / gaps
		if i < free%gaps {
			n++
		}
		lc := p.Line
		if s.State == StepDone || s.State == StepSkipped {
			lc = Mix(bg, p.Accent, .6)
		}
		l.Sp(1).T(strings.Repeat(G.H, max(1, n-2)), lc).Sp(1)
	}
	return l.Pad(w)
}
