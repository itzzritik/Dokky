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

// the list keys: up/down by one, pgup/pgdown by a page, home/end to the ends.
// Reports whether k was one of them, so the caller can stop there.
func Move(k string, sel, n, page int) (int, bool) {
	to := sel
	switch k {
	case "up":
		to--
	case "down":
		to++
	case "pgup":
		to -= max(1, page)
	case "pgdown":
		to += max(1, page)
	case "home":
		to = 0
	case "end":
		to = n - 1
	default:
		return sel, false
	}
	return max(0, min(to, n-1)), true
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

// a solid card with half-block top and bottom edges; rows get the fill and the inner width
func Tile(id string, w int, selected bool, rows func(bg color.Color, iw int) []string) *lipgloss.Layer {
	return Card(id, w, selected, P().Surface, nil, rows)
}

// a card in fill over under (nil on the screen, the window colour inside one)
func Card(id string, w int, selected bool, fill, under color.Color, rows func(bg color.Color, iw int) []string) *lipgloss.Layer {
	p := P()
	bg := fill
	if selected {
		bg = p.Sel
	}
	edge := func(g string) string {
		if G.ASCII {
			return Paint(strings.Repeat(" ", w), nil, bg, false)
		}
		return Paint(strings.Repeat(g, w), bg, under, false)
	}
	bar := Paint(" ", nil, bg, false)
	if selected {
		bar = Paint(G.Bar, p.Accent, bg, false)
	}
	lines := []string{edge(G.Top)}
	// one cell on the right matches the half row above and below
	for _, r := range rows(bg, w-3) {
		lines = append(lines, L(bg).Raw(bar).Sp(1).Raw(r).Pad(w))
	}
	lines = append(lines, edge(G.Bottom))
	return lipgloss.NewLayer(strings.Join(lines, "\n")).ID(id)
}

// a tinted band, coral when something is broken, else buoy; the fix key sits on the right
func Banner(w, h int, bad bool, text, key, action string) []string {
	p := P()
	c, glyph, bg := band(bad)
	right := bannerKey(bg, w, key, action)
	if key != "" {
		right.Sp(2)
	}
	lines := make([]string, h)
	for i := range lines {
		l := L(bg).Raw(Paint(G.Bar, c, bg, false))
		if i != h/2 {
			lines[i] = l.Pad(w)
			continue
		}
		lines[i] = l.Sp(2).B(glyph, c).Sp(1).T(text, p.Text).Right(w, right)
	}
	return lines
}

func band(bad bool) (c color.Color, glyph string, bg color.Color) {
	p := P()
	c, glyph = p.Buoy, G.Bang
	if bad {
		c, glyph = p.Coral, G.Cross
	}
	return c, glyph, Mix(p.Bg, c, .12)
}

func bannerKey(bg color.Color, w int, key, action string) *Line {
	right := L(bg)
	if key != "" {
		right.Raw(Keycap(key))
		if action != "" && w >= 60 {
			right.Sp(1).B(action, P().Text)
		}
	}
	return right
}

// a Banner drawn at y whose key can be clicked like a footer hint
func PutBanner(b *Box, y, w, h int, bad bool, text, key, action string) {
	b.Lines(0, y, Banner(w, h, bad, text, key, action))
	if key == "" {
		return
	}
	_, _, bg := band(bad)
	k := bannerKey(bg, w, key, action)
	b.Hit(w-2-k.Width(), y+h/2, "key:"+key, k.String())
}
