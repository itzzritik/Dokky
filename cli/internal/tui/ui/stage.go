package ui

import (
	"image"
	"strings"
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

// Band is the animated backdrop behind a stage, filling the whole screen
type Band interface {
	Size() (w, rows int)
	Step()
	Lines() []string
}

// a band that keeps quiet where text sits: the rectangles are in cells
type Calmer interface{ Calm([]image.Rectangle) }

// Stage is Forged's gate layout over a full-screen band: the wordmark, and one card in the middle. New makes the
// band for a size; nil, or a screen too small, leaves the band out. One stage can sit behind every full screen,
// so the band carries on as screens change
type Stage struct {
	New  func(w, rows int) Band
	band Band
	last time.Time // when the band last stepped
}

// the band moves at 20 frames a second whatever the redraw rate
const bandFrame = 50 * time.Millisecond

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
func (s *Stage) Moving() bool { return s != nil && s.band != nil && !Reduced() }

// Behind draws the band across a w by h screen, quiet in the calm rectangles where text sits; nil when there is
// no band or the screen is too small for one
func (s *Stage) Behind(w, h int, now time.Time, calm ...image.Rectangle) *lipgloss.Layer {
	if s == nil || s.New == nil || w < 40 || h < 16 {
		if s != nil {
			s.band = nil
		}
		return nil
	}
	if bw, bh := s.bandSize(); bw != w || bh != h {
		s.band = s.New(w, h)
	}
	if c, ok := s.band.(Calmer); ok {
		c.Calm(calm)
	}
	s.advance(now)
	// beneath everything, windows' own fill included
	return lipgloss.NewLayer(strings.Join(s.band.Lines(), "\n")).Z(-100)
}

// Layer places the card, built for at most maxH rows, in the middle, the wordmark above it and the band behind
func (s *Stage) Layer(w, h int, now time.Time, card func(maxH int) *Window) *lipgloss.Layer {
	win := card(h - 2)
	cardY, cardX := max(1, (h-win.H)/2), (w-win.W)/2
	calm := []image.Rectangle{image.Rect(cardX, cardY, cardX+win.W, cardY+win.H)}
	b := NewBox()
	var wm []string
	var mx, my int
	switch room := cardY - 1; {
	case room >= 6:
		wm = Wordmark()
		mx, my = (w-Width(wm[0]))/2, max(1, max((cardY-3)/2, cardY-7))
	case room >= 3:
		wm = []string{Paint("dokky", P().Text, nil, true)}
		mx, my = (w-5)/2, cardY-2
	}
	if wm != nil {
		calm = append(calm, image.Rect(mx, my, mx+Width(wm[0]), my+len(wm)))
	}
	if l := s.Behind(w, h, now, calm...); l != nil {
		b.Add(0, 0, l)
	}
	if wm != nil {
		b.Lines(mx, my, wm)
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

// Ink is the smallest rectangle, in cells, holding everything l draws on a w by h screen, so a band can keep
// quiet behind it; empty when l draws nothing
func Ink(l *lipgloss.Layer, w, h int) image.Rectangle {
	c := lipgloss.NewCanvas(w, h)
	c.Compose(lipgloss.NewCompositor(l))
	r := image.Rectangle{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if cell := c.CellAt(x, y); cell != nil && cell.Content != "" && cell.Content != " " {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return r
}
