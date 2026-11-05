package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

// content sits inside a 1 cell margin, the border and 2 cells of padding
const WindowPad = 4

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
	Ok, Bad, SubBad bool
	Body            *Box
	Buttons         []Btn
	Active          int
}

func (w *Window) Inner() int { return w.W - 2*WindowPad }

func (w *Window) top() int {
	t := 3
	if len(w.Steps) > 0 {
		t += 2
	}
	if w.Sub != "" {
		t += 2
	}
	return t
}

func (w *Window) bottom() int {
	if len(w.Buttons) > 0 {
		return 5
	}
	return 3
}

// sizes the window to its body rows, never taller than maxH; with no body the text's gap is the only one
func (w *Window) Fit(rows, maxH int) {
	h := w.top() + rows + w.bottom()
	if rows == 0 && w.top() > 3 {
		h--
	}
	w.H = min(maxH, h)
}

// body rows that fit in maxH
func (w *Window) Room(maxH int) int { return max(1, maxH-w.top()-w.bottom()) }

func (w *Window) Layer() *lipgloss.Layer {
	p := P()
	bg := p.Surface
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
	title.B(core.Fit(w.Title, w.W-12), tc)
	tl := L(bg).Sp(1).Raw(title.String()).Sp(1)
	run := max(0, w.W-6-tl.Width())
	top := blank(1) + line(G.TL+G.H) + tl.String() + line(strings.Repeat(G.H, run)) + line(G.H+G.TR) + blank(1)

	fill := make([]string, w.H)
	for i := range fill {
		switch i {
		case 0:
			fill[i] = Paint(strings.Repeat(G.Top, w.W), bg, nil, false)
		case 1:
			fill[i] = top
		case w.H - 2:
			fill[i] = blank(1) + line(G.BL+strings.Repeat(G.H, w.W-4)+G.BR) + blank(1)
		case w.H - 1:
			fill[i] = Paint(strings.Repeat(G.Bottom, w.W), bg, nil, false)
		default:
			fill[i] = blank(1) + line(G.V) + blank(w.W-4) + line(G.V) + blank(1)
		}
	}
	if G.ASCII {
		fill[0], fill[w.H-1] = blank(w.W), blank(w.W)
	}
	root := lipgloss.NewLayer(strings.Join(fill, "\n")).Z(-1)

	b := NewBox()
	y := 3
	if len(w.Steps) > 0 {
		b.Put(WindowPad, y, stepper(w.Steps, cw, bg))
		y += 2
	}
	if w.Sub != "" {
		sc := p.Muted
		if w.SubBad {
			sc = p.Coral
		}
		b.Put(WindowPad, y, L(bg).T(w.Sub, sc).Pad(cw))
	}

	box := lipgloss.NewLayer("").AddLayers(root, b.Layer())
	if w.Body != nil {
		box.AddLayers(w.Body.Layer().X(WindowPad).Y(w.top()))
	}
	if len(w.Buttons) > 0 {
		bar := NewBox()
		x := 0
		for i, btn := range w.Buttons {
			bar.Add(x, 0, Button("btn:"+btn.ID, btn.Label, btn.Primary, btn.Danger, i == w.Active))
			x += ButtonWidth(btn.Label) + 2
		}
		box.AddLayers(bar.Layer().X(WindowPad + cw - (x - 2)).Y(w.H - 4))
	}
	return box
}

// a text row inside a window
func (w *Window) Text(s string, fg color.Color) string { return L(P().Surface).T(s, fg).Pad(w.Inner()) }

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
