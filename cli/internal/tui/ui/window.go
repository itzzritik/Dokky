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

// every modal: a surface with a rounded border, the title in its top edge, text, body, buttons at the bottom right
type Window struct {
	W, H            int
	Title, Sub      string
	Step            string
	Progress        float64
	Ok, Bad, SubBad bool
	Body            *Box
	Shimmer         float64
	Buttons         []Btn
	Active          int
}

func (w *Window) Inner() int { return w.W - 2*WindowPad }

func (w *Window) top() int {
	t := 3
	if w.Step != "" {
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
	rl := L(bg)
	if w.Step != "" {
		rl.Sp(1).T(w.Step, p.Faint).Sp(1)
	}
	run := max(0, w.W-6-tl.Width()-rl.Width())
	top := blank(1) + line(G.TL+G.H) + tl.String() + line(strings.Repeat(G.H, run)) + rl.String() + line(G.H+G.TR) + blank(1)

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
	if w.Step != "" {
		var bar strings.Builder
		n := int(float64(cw)*w.Progress + .5)
		for i := 0; i < cw; i++ {
			c := Mix(p.Surface, p.Text, .12)
			if i < n {
				c = Mix(p.Accent, p.Ramp[4], Wave(i, .3, 5, w.Shimmer))
			}
			bar.WriteString(Paint(G.Heavy, c, bg, false))
		}
		b.Put(WindowPad, y, bar.String())
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
