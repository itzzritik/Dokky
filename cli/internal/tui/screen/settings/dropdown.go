package settings

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// a choice row's options, under (or over) the row, flush right
type dropdown struct {
	title       string
	items       []string
	cur, cursor int
	anchor      int
	choose      func(*core.State, int) tea.Cmd
}

func (d *dropdown) Place(bw, bh, w, h int) (int, int) {
	y := d.anchor + 1
	if y+h > bh {
		y = max(0, d.anchor-h)
	}
	return bw - w, y
}

func (d *dropdown) pick(st *core.State, i int) tea.Cmd {
	if i == d.cur {
		return core.Close(d)
	}
	return tea.Batch(core.Close(d), d.choose(st, i))
}

func (d *dropdown) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "opt:")); err == nil && i < len(d.items) {
			return d, d.pick(st, i)
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return d, core.Close(d)
		case "up":
			d.cursor = max(0, d.cursor-1)
		case "down":
			d.cursor = min(len(d.items)-1, d.cursor+1)
		case "enter", "space":
			return d, d.pick(st, d.cursor)
		}
	}
	return d, nil
}

func (d *dropdown) View(_ *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	w := max(28, ui.Width(d.title)+12)
	for _, it := range d.items {
		w = max(w, ui.Width(it)+14)
	}
	win := &ui.Window{W: min(w, maxW), Title: d.title}
	body := ui.NewBox()
	for i, it := range d.items {
		body.Add(0, i, ui.Row("opt:"+strconv.Itoa(i), win.Inner(), i == d.cursor, p.Surface, func(bg color.Color, iw int) string {
			l := ui.L(bg)
			if i == d.cursor {
				l.B(it, p.Text)
			} else {
				l.T(it, p.Text)
			}
			mark := ui.L(bg)
			if i == d.cur {
				mark.B(ui.G.Check, p.Kelp)
			}
			return l.Right(iw-1, mark)
		}))
	}
	win.Body = body
	win.Fit(len(d.items), maxH)
	return win.Layer()
}

func (d *dropdown) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "close"}}
}

func (d *dropdown) Animating(*core.State) bool { return false }
