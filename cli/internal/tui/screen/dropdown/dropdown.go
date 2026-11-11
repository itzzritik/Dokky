// Package dropdown is Forged's small list that opens under the row it belongs to
package dropdown

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const width = 24

// a choice list anchored under a body row, right-aligned, the current value ticked
type Dropdown struct {
	Items  []string
	Cur    int                 // the value now, ticked; -1 for none
	Anchor int                 // the body row it opens under
	Choose func(i int) tea.Cmd // runs once it has closed; nil when the current value is picked again
	cursor int
}

// Open shows d with the current value under the cursor
func Open(d *Dropdown) tea.Cmd {
	d.cursor = max(0, d.Cur)
	return core.Open(d)
}

// under its row, or above it when the body has no room below
func (d *Dropdown) Place(bw, bh, w, h int) (int, int) {
	y := d.Anchor + 1
	if y+h > bh {
		y = max(0, d.Anchor-h)
	}
	return max(0, bw-w), y
}

func (d *Dropdown) pick(i int) tea.Cmd {
	if i == d.Cur || d.Choose == nil {
		return core.Close(d)
	}
	return tea.Sequence(core.Close(d), d.Choose(i))
}

func (d *Dropdown) Update(msg tea.Msg, _ *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "dd:")); err == nil && i < len(d.Items) {
			return d, d.pick(i)
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return d, core.Close(d)
		case "up":
			d.cursor = max(0, d.cursor-1)
		case "down":
			d.cursor = min(len(d.Items)-1, d.cursor+1)
		case "enter", "space":
			return d, d.pick(d.cursor)
		}
	}
	return d, nil
}

func (d *Dropdown) View(_ *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(width, maxW), Flush: true}
	iw, bg := win.Edge(), p.Window
	var lines []ui.ListLine
	for i, it := range d.Items {
		on := i == d.cursor
		row := ui.RowLine(iw, on, bg, func(bg color.Color, w int) string {
			l := ui.L(bg).Sp(1)
			if on {
				l.B(it, p.Text)
			} else {
				l.T(it, p.Text)
			}
			mark := ui.L(bg)
			if i == d.Cur {
				mark.T(ui.G.Check, p.Kelp)
			}
			return l.Right(w-1, mark)
		})
		lines = append(lines, ui.ListLine{Text: row, Item: i, ID: "dd:" + strconv.Itoa(i)})
	}
	rows := min(len(lines), win.Room(maxH))
	body := ui.NewBox()
	top := 0
	ui.List(body, 0, rows, &top, d.cursor, lines)
	win.Body = body
	win.Fit(rows, maxH)
	return win.Layer()
}

func (d *Dropdown) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "↑↓", Label: "Choose"}, {Key: "enter", Label: "Select"}, {Key: "esc", Label: "Close"}}
}

func (d *Dropdown) Animating(*core.State) bool { return false }
