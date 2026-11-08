package ui

import (
	"time"

	"charm.land/lipgloss/v2"
)

const (
	toastFor   = 3 * time.Second
	badFor     = 6 * time.Second
	toastSlide = 200 * time.Millisecond
)

type Toast struct {
	Text string
	Bad  bool
	Now  bool
	At   time.Time
}

func (t Toast) For() time.Duration {
	if t.Bad {
		return badFor
	}
	return toastFor
}

func (t Toast) Live(now time.Time) bool { return now.Sub(t.At) < t.For() }

func (t Toast) Moving(now time.Time) bool { return Easing(t.At, now, toastSlide) }

// bottom right, sliding in from the edge
func (t Toast) Layer(w, y int, now time.Time) *lipgloss.Layer {
	p := P()
	bar := p.Accent
	if t.Bad {
		bar = p.Coral
	}
	s := L(p.Cap).Raw(Paint(G.Toast, bar, p.Cap, false)).Sp(1).T(t.Text, p.Text).Sp(2)
	tw := min(s.Width(), w-6)
	x := w - 3 - tw + int(Ease(float64(tw+3), 0, t.At, now, toastSlide))
	return lipgloss.NewLayer(s.Pad(tw)).X(x).Y(y).ID("toast")
}
