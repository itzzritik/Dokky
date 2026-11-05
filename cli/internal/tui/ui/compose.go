package ui

import "charm.land/lipgloss/v2"

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

func Compose(w, h int, passes ...Pass) (string, Hits) {
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
	return c.Render(), hits
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
