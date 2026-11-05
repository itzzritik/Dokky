package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// a solid card with half-block top and bottom edges; rows get the fill and the inner width
func Tile(id string, w int, selected bool, rows func(bg color.Color, iw int) []string) *lipgloss.Layer {
	p := P()
	bg := p.Surface
	if selected {
		bg = p.Sel
	}
	edge := func(g string) string {
		if G.ASCII {
			return Paint(strings.Repeat(" ", w), nil, bg, false)
		}
		return Paint(strings.Repeat(g, w), bg, nil, false)
	}
	lines := []string{edge(G.Top)}
	bar := " "
	if selected {
		bar = Paint(G.Bar, p.Accent, bg, false)
	} else {
		bar = Paint(bar, nil, bg, false)
	}
	for _, r := range rows(bg, w-4) {
		lines = append(lines, L(bg).Raw(bar).Sp(1).Raw(r).Pad(w))
	}
	lines = append(lines, edge(G.Bottom))
	l := lipgloss.NewLayer(strings.Join(lines, "\n"))
	if id != "" {
		l.ID(id)
	}
	return l
}

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
