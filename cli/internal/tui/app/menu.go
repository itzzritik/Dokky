package app

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const menuW = 34

// every action of the screen under it, then the global ones
type menu struct {
	items       []core.Action // a blank Key is a separator
	cursor, top int
	owner       core.Screen
}

type menuRun struct {
	owner core.Screen
	key   string
}

func newMenu(owner core.Screen, acts, global []core.Action) *menu {
	m := &menu{owner: owner}
	seen := map[string]bool{}
	add := func(a core.Action) {
		if !seen[a.Key] && core.Press(a.Key).Code != 0 {
			seen[a.Key] = true
			m.items = append(m.items, a)
		}
	}
	for _, a := range acts {
		add(a)
	}
	if len(m.items) > 0 && len(global) > 0 {
		m.items = append(m.items, core.Action{})
	}
	for _, a := range global {
		add(a)
	}
	return m
}

func (a *App) openMenu() tea.Cmd {
	var global []core.Action
	if len(a.windows) == 0 {
		global = a.globals()
	}
	if m := newMenu(a.top(), a.top().Actions(a.st), global); len(m.items) > 0 {
		a.windows = append(a.windows, m)
	}
	return nil
}

func (m *menu) Place(bw, bh, w, h int) (int, int) { return bw - w, bh - h }

func (m *menu) run(key string) tea.Cmd {
	return tea.Sequence(core.Close(m), core.Send(menuRun{m.owner, key}))
}

func (m *menu) Update(msg tea.Msg, _ *core.State) (core.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(msg.ID, "menu:")); err == nil && i < len(m.items) {
			return m, m.run(m.items[i].Key)
		}
	case tea.KeyPressMsg:
		k := strings.ToLower(msg.String())
		switch k {
		case "enter":
			return m, m.run(m.items[m.cursor].Key)
		case "esc", "m":
			return m, core.Close(m)
		case "up":
			m.move(-1)
		case "down":
			m.move(1)
		default:
			for _, it := range m.items {
				if it.Key == k {
					return m, m.run(k)
				}
			}
		}
	}
	return m, nil
}

func (m *menu) move(d int) {
	for i := m.cursor + d; i >= 0 && i < len(m.items); i += d {
		if m.items[i].Key != "" {
			m.cursor = i
			return
		}
	}
}

func (m *menu) View(_ *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(menuW, maxW), Title: "Actions"}
	iw := win.Inner()
	var lines []ui.ListLine
	for i, it := range m.items {
		if it.Key == "" {
			lines = append(lines, ui.ListLine{Text: ui.Paint(strings.Repeat(ui.G.H, iw), p.Line, p.Surface, false), Item: -1})
			continue
		}
		row := ui.RowLine(iw, i == m.cursor, p.Surface, func(bg color.Color, w int) string {
			fg := p.Text
			if it.Danger {
				fg = p.Coral
			}
			label := it.Label
			if label != "" {
				label = strings.ToUpper(label[:1]) + label[1:]
			}
			l := ui.L(bg)
			if i == m.cursor {
				l.B(label, fg)
			} else {
				l.T(label, fg)
			}
			return l.Right(w-1, ui.L(bg).B(it.Key, p.Muted))
		})
		lines = append(lines, ui.ListLine{Text: row, Item: i, ID: "menu:" + strconv.Itoa(i)})
	}
	rows := min(len(lines), win.Room(maxH))
	body := ui.NewBox()
	ui.List(body, 0, rows, &m.top, m.cursor, lines)
	win.Body = body
	win.Fit(rows, maxH)
	return win.Layer()
}

func (m *menu) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "run"}, {Key: "esc", Label: "close"}}
}

func (m *menu) Animating(*core.State) bool { return false }
