package ui

import (
	"image"

	"charm.land/lipgloss/v2"
)

// drawn in order; Dim fades what is already drawn, Modal takes every click below it
type Pass struct {
	Layer      *lipgloss.Layer
	Dim, Modal bool
}

type Hits []hitPass

type hitPass struct {
	c     *lipgloss.Compositor
	modal bool
}

func (h Hits) At(x, y int) string {
	for i := len(h) - 1; i >= 0; i-- {
		if hit := h[i].c.Hit(x, y); !hit.Empty() {
			return hit.ID()
		}
		if h[i].modal {
			return ""
		}
	}
	return ""
}

// the screen, its click targets, and the caret of the field being typed in (nil when none shows)
func Compose(w, h int, passes ...Pass) (string, Hits, *image.Point) {
	c := lipgloss.NewCanvas(w, h)
	var hits Hits
	for _, p := range passes {
		if p.Layer == nil {
			continue
		}
		if p.Dim {
			dim(c, w, h)
		}
		comp := lipgloss.NewCompositor(p.Layer)
		c.Compose(comp)
		hits = append(hits, hitPass{comp, p.Modal})
	}
	at := caret(c, w, h)
	return c.Render(), hits, at
}

// a dimmed field lost its ink, so only the live one is found
func caret(c *lipgloss.Canvas, w, h int) *image.Point {
	r, g, b, _ := caretInk.RGBA()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := c.CellAt(x, y)
			if cell == nil || cell.Style.Fg == nil {
				continue
			}
			if cr, cg, cb, _ := cell.Style.Fg.RGBA(); cr == r && cg == g && cb == b {
				cell.Style.Fg = caretFg
				return &image.Point{X: x, Y: y}
			}
		}
	}
	return nil
}

func dim(c *lipgloss.Canvas, w, h int) {
	bg := P().Bg
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := c.CellAt(x, y)
			if cell == nil {
				continue
			}
			fg := cell.Style.Fg
			if fg == nil {
				fg = P().Text
			}
			cell.Style.Fg = Mix(bg, fg, .22)
			if cell.Style.Bg != nil {
				cell.Style.Bg = Mix(bg, cell.Style.Bg, .22)
			}
			cell.Style.Attrs = 0
		}
	}
}
