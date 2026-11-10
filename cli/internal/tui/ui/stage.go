package ui

import (
	"image/color"
	"time"

	"charm.land/lipgloss/v2"
)

// Wordmark is dokky's still logo in Forged's pixel letters, three rows tall
func Wordmark() []string {
	if G.ASCII {
		return []string{"", Paint("D O K K Y", P().Text, nil, true), ""}
	}
	rows := []string{
		"█▀▄ █▀█ █ █ █ █ █ █",
		"█ █ █ █ █▀▄ █▀▄ ▀█▀",
		"▀▀  ▀▀▀ ▀ ▀ ▀ ▀  ▀ ",
	}
	for i, r := range rows {
		rows[i] = Paint(r, P().Text, nil, false)
	}
	return rows
}

// a speck drawn above the band, like spray off the water
type Particle struct {
	X, Row int
	Glyph  string
	Fg     color.Color
}

// Band is the animated strip along the bottom of a stage
type Band interface {
	Size() (w, rows int)
	Step()
	Lines() []string
	// Particles above the band; top is the band's first screen row
	Particles(top int) []Particle
}

// Stage is Forged's gate layout: the wordmark, one card in the middle and a band filling the bottom third.
// New makes the band for a size; nil, or a screen too small, leaves the band out
type Stage struct {
	New  func(w, rows int) Band
	band Band
	last time.Time // when the band last stepped
}

// the band moves at 20 frames a second whatever the redraw rate
const bandFrame = 50 * time.Millisecond

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// CardWidth is how wide the stage's card is on a screen w wide
func CardWidth(w int) int { return min(52, w-4) }

// advance steps the band for the time passed since the last draw, a few frames at most
func (s *Stage) advance(now time.Time) {
	if s.band == nil || Reduced() {
		return
	}
	if s.last.IsZero() || now.Before(s.last) {
		s.last = now
		return
	}
	n := int(now.Sub(s.last) / bandFrame)
	for range min(n, 3) {
		s.band.Step()
	}
	if n > 3 {
		s.last = now
	} else {
		s.last = s.last.Add(time.Duration(n) * bandFrame)
	}
}

// Moving says whether the band animates, so the app keeps ticking
func (s *Stage) Moving() bool { return s.band != nil && !Reduced() }

// Layer places the card, built for at most maxH rows, above the band, and the wordmark above the card
func (s *Stage) Layer(w, h int, now time.Time, card func(maxH int) *Window) *lipgloss.Layer {
	top := -1
	if s.New != nil && w >= 40 && h >= 16 {
		top = h - int(float64(h)*0.33+0.5)
	}
	lim := h
	if top >= 0 {
		lim = top
	}
	win := card(lim - 2)
	if win.H > lim-2 && top >= 0 {
		top, lim = -1, h
		win = card(h - 2)
	}
	cardY := (h - win.H) / 2
	if cardY+win.H > lim-1 {
		cardY = lim - 1 - win.H
	}
	cardY = max(1, cardY)
	cardX := (w - win.W) / 2
	b := NewBox()
	if top >= 0 {
		if bw, br := s.bandSize(); bw != w || br != h-top {
			s.band = s.New(w, h-top)
		}
		s.advance(now)
		b.Lines(0, top, s.band.Lines())
	} else {
		s.band = nil
	}
	var mark rect
	switch room := cardY - 1; {
	case room >= 6:
		wm := Wordmark()
		mw := Width(wm[0])
		mark = rect{(w - mw) / 2, max(1, max((cardY-3)/2, cardY-7)), mw, len(wm)}
		b.Lines(mark.x, mark.y, wm)
	case room >= 3:
		mark = rect{(w - 5) / 2, cardY - 2, 5, 1}
		b.Put(mark.x, mark.y, Paint("dokky", P().Text, nil, true))
	}
	cr := rect{cardX, cardY, win.W, win.H}
	if top >= 0 {
		for _, pt := range s.band.Particles(top) {
			if pt.X < 0 || pt.X >= w || pt.Row < 0 || pt.Row >= top || cr.has(pt.X, pt.Row) || mark.has(pt.X, pt.Row) {
				continue
			}
			b.Put(pt.X, pt.Row, Paint(pt.Glyph, pt.Fg, nil, false))
		}
	}
	b.Add(cardX, cardY, win.Layer())
	return b.Layer()
}

func (s *Stage) bandSize() (int, int) {
	if s.band == nil {
		return 0, 0
	}
	return s.band.Size()
}
