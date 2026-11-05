package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	WindowPad = 5
	bodyTop   = 7
)

// a surface card over a dimmed screen: progress line, title, one line, then the body from row 7
type Window struct {
	W, H       int
	Title, Sub string
	Step       string
	Progress   float64
	Ok, Bad    bool
	SubBad     bool
	Body       *Box
	Shimmer    float64
}

func (w *Window) Inner() int { return w.W - 2*WindowPad }

func (w *Window) Layer() *lipgloss.Layer {
	p := P()
	bg := p.Surface
	fill := make([]string, w.H)
	for i := range fill {
		switch {
		case i == 0 && !G.ASCII:
			fill[i] = Paint(strings.Repeat(G.Top, w.W), bg, nil, false)
		case i == w.H-1 && !G.ASCII:
			fill[i] = Paint(strings.Repeat(G.Bottom, w.W), bg, nil, false)
		default:
			fill[i] = Paint(strings.Repeat(" ", w.W), nil, bg, false)
		}
	}
	root := lipgloss.NewLayer(strings.Join(fill, "\n")).Z(-1)
	b := NewBox()
	cw := w.Inner()
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
		b.Put(WindowPad, 2, bar.String())
	}
	title := L(bg)
	tc := p.Text
	switch {
	case w.Ok:
		title.B(G.Check+" ", p.Kelp)
	case w.Bad:
		tc = p.Coral
	}
	title.B(w.Title, tc)
	right := L(bg)
	if w.Step != "" {
		right.T(w.Step, p.Faint)
	}
	b.Put(WindowPad, 4, title.Right(cw, right))
	if w.Sub != "" {
		sc := p.Muted
		if w.SubBad {
			sc = p.Coral
		}
		b.Put(WindowPad, 5, L(bg).T(w.Sub, sc).Pad(cw))
	}
	box := lipgloss.NewLayer("").AddLayers(root, b.Layer())
	if w.Body != nil {
		box.AddLayers(w.Body.Layer().X(WindowPad).Y(bodyTop))
	}
	return box
}

// a text row inside a window
func (w *Window) Text(s string, fg color.Color) string { return L(P().Surface).T(s, fg).Pad(w.Inner()) }
