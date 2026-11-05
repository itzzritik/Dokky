package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
)

func Width(s string) int { return ansi.StringWidth(s) }

func style(fg, bg color.Color, bold bool) lipgloss.Style {
	s := lipgloss.NewStyle().Bold(bold)
	if fg != nil {
		s = s.Foreground(fg)
	}
	if bg != nil {
		s = s.Background(bg)
	}
	return s
}

func Paint(s string, fg, bg color.Color, bold bool) string {
	if s == "" {
		return ""
	}
	return style(fg, bg, bold).Render(s)
}

// one row of styled pieces on a shared background
type Line struct {
	bg color.Color
	b  strings.Builder
	w  int
}

func L(bg color.Color) *Line { return &Line{bg: bg} }

func (l *Line) T(s string, fg color.Color) *Line { return l.Raw(Paint(core.Safe(s), fg, l.bg, false)) }

func (l *Line) B(s string, fg color.Color) *Line { return l.Raw(Paint(core.Safe(s), fg, l.bg, true)) }

func (l *Line) Sp(n int) *Line {
	if n > 0 {
		l.Raw(Paint(strings.Repeat(" ", n), nil, l.bg, false))
	}
	return l
}

func (l *Line) Raw(s string) *Line {
	l.b.WriteString(s)
	l.w += Width(s)
	return l
}

func (l *Line) Width() int     { return l.w }
func (l *Line) String() string { return l.b.String() }

// pads with the background, or cuts with an ellipsis, to exactly w
func (l *Line) Pad(w int) string {
	if l.w > w {
		cut := ansi.Truncate(l.b.String(), w, G.Ellipsis)
		// a wide character that does not fit leaves one cell to fill
		return cut + Paint(strings.Repeat(" ", max(0, w-Width(cut))), nil, l.bg, false)
	}
	return l.b.String() + Paint(strings.Repeat(" ", w-l.w), nil, l.bg, false)
}

// left side cut to make room, right side flush to w
func (l *Line) Right(w int, r *Line) string {
	if r == nil || r.w == 0 {
		return l.Pad(w)
	}
	left := max(0, w-r.w-1)
	return L(l.bg).Raw(l.Pad(left)).Sp(w - left - r.w).Raw(r.String()).Pad(w)
}

// clickable and plain pieces placed by cell
type Box struct {
	root *lipgloss.Layer
}

func NewBox() *Box { return &Box{root: lipgloss.NewLayer("")} }

func (b *Box) Put(x, y int, s string) *Box {
	if s != "" {
		b.root.AddLayers(lipgloss.NewLayer(s).X(x).Y(y))
	}
	return b
}

func (b *Box) Hit(x, y int, id, s string) *Box {
	b.root.AddLayers(lipgloss.NewLayer(s).X(x).Y(y).ID(id))
	return b
}

func (b *Box) Lines(x, y int, lines []string) *Box {
	return b.Put(x, y, strings.Join(lines, "\n"))
}

func (b *Box) Add(x, y int, l *lipgloss.Layer) *Box {
	if l != nil {
		b.root.AddLayers(l.X(x).Y(y))
	}
	return b
}

func (b *Box) Layer() *lipgloss.Layer { return b.root }
