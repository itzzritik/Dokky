package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// one list row on base (nil outside windows): selection fill and accent bar, content from column 2
func Row(id string, w int, selected bool, base color.Color, content func(bg color.Color, iw int) string) *lipgloss.Layer {
	return lipgloss.NewLayer(RowLine(w, selected, base, content)).ID(id)
}

func RowLine(w int, selected bool, base color.Color, content func(bg color.Color, iw int) string) string {
	p := P()
	bg, bar := base, Paint(" ", nil, base, false)
	if selected {
		bg = p.Sel
		bar = Paint(G.Bar, p.Accent, bg, false)
	}
	return L(bg).Raw(bar).Sp(1).Raw(content(bg, w-3)).Pad(w)
}

// an outlined panel for information, title in the top border
func Panel(w, h int, title, right string, body []string) string {
	p := P()
	line := func(s string) string { return Paint(s, p.Line, nil, false) }
	top := L(nil).Raw(line(G.TL+G.H)).T(" "+title+" ", p.Muted)
	tail := L(nil)
	if right != "" {
		tail.T(" "+right+" ", p.Muted)
	}
	tail.Raw(line(G.H + G.TR))
	fill := max(0, w-top.Width()-tail.Width())
	out := []string{top.Raw(line(strings.Repeat(G.H, fill))).Raw(tail.String()).Pad(w)}
	for i := 0; i < h-2; i++ {
		s := ""
		if i < len(body) {
			s = body[i]
		}
		out = append(out, L(nil).Raw(line(G.V)).Sp(1).Raw(L(nil).Raw(s).Pad(w-4)).Sp(1).Raw(line(G.V)).String())
	}
	return strings.Join(append(out, line(G.BL+strings.Repeat(G.H, max(0, w-2))+G.BR)), "\n")
}

// a list line; Item < 0 is a header or a blank, others are clickable rows
type ListLine struct {
	Text string
	Item int
	ID   string
}

// draws h lines from y, scrolled so the selected item (and the header above it) shows
func List(b *Box, y, h int, top *int, sel int, lines []ListLine) {
	at := 0
	for i, l := range lines {
		if l.Item == sel && l.Item >= 0 {
			at = i
		}
	}
	if at <= *top {
		*top = max(0, at-1)
	}
	if at >= *top+h {
		*top = at - h + 1
	}
	*top = max(0, min(*top, len(lines)-h))
	for i := *top; i < len(lines) && i < *top+h; i++ {
		if l := lines[i]; l.ID != "" {
			b.Hit(0, y+i-*top, l.ID, l.Text)
		} else {
			b.Put(0, y+i-*top, l.Text)
		}
	}
}
