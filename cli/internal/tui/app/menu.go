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

// a window that names its buttons, so the menu lists them by label and presses them
type buttoned interface {
	Buttons(*core.State) []ui.Btn
}

// one menu line: a key to send, or a button of the window under the menu to press
type menuItem struct {
	core.Action
	btn string // the button's click ID, "" for a key
}

// a blank line between the screen's items and the global ones
func (it menuItem) sep() bool { return it.Key == "" && it.btn == "" }

// every action of the screen under it, then the global ones
type menu struct {
	items       []menuItem
	cursor, top int
	owner       core.Screen
}

type menuRun struct {
	owner core.Screen
	key   string
}

// keys that only move focus, which means nothing once the menu has closed
var moves = map[string]bool{"tab": true, "shift+tab": true, "up": true, "down": true, "left": true, "right": true}

// a window's buttons come first, by label, each showing a key that does the same; then the keys no button covers
func newMenu(owner core.Screen, st *core.State, global []core.Action) *menu {
	m := &menu{owner: owner}
	acts := owner.Actions(st)
	seen, named := map[string]bool{}, map[string]bool{}
	if b, ok := owner.(buttoned); ok {
		for _, btn := range b.Buttons(st) {
			key := sameKey(acts, btn.Label)
			seen[key] = key != ""
			named[strings.ToLower(btn.Label)] = true
			m.items = append(m.items, menuItem{Action: core.Action{Key: key, Label: btn.Label, Danger: btn.Danger}, btn: "btn:" + btn.ID})
		}
	}
	add := func(a core.Action) {
		if seen[a.Key] || named[strings.ToLower(a.Label)] || moves[a.Key] || core.Press(a.Key).Code == 0 {
			return
		}
		seen[a.Key] = true
		m.items = append(m.items, menuItem{Action: a})
	}
	for _, a := range acts {
		add(a)
	}
	if len(m.items) > 0 && len(global) > 0 {
		m.items = append(m.items, menuItem{})
	}
	for _, a := range global {
		add(a)
	}
	return m
}

// the key that does what the button does, a letter or esc before enter, which follows focus
func sameKey(acts []core.Action, label string) string {
	key := ""
	for _, a := range acts {
		if !strings.EqualFold(a.Label, label) || core.Press(a.Key).Code == 0 {
			continue
		}
		if a.Key != "enter" {
			return a.Key
		}
		key = a.Key
	}
	return key
}

func (a *App) openMenu() tea.Cmd {
	var global []core.Action
	if len(a.windows) == 0 {
		global = a.globals()
	}
	if m := newMenu(a.top(), a.st, global); len(m.items) > 0 {
		a.windows = append(a.windows, m)
	}
	return nil
}

func (m *menu) Place(bw, bh, w, h int) (int, int) { return bw - w, bh - h }

// a button is pressed as a click on the window under the menu, which is the one that opened it
func (m *menu) run(it menuItem, st *core.State) tea.Cmd {
	if it.btn != "" {
		_, cmd := m.owner.Update(core.Click{ID: it.btn}, st)
		return tea.Sequence(core.Close(m), cmd)
	}
	return tea.Sequence(core.Close(m), core.Send(menuRun{m.owner, it.Key}))
}

func (m *menu) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(msg.ID, "menu:")); err == nil && i < len(m.items) {
			return m, m.run(m.items[i], st)
		}
	case tea.KeyPressMsg:
		k := strings.ToLower(msg.String())
		switch k {
		case "enter":
			return m, m.run(m.items[m.cursor], st)
		case "esc", "?":
			return m, core.Close(m)
		case "up":
			m.move(-1)
		case "down":
			m.move(1)
		default:
			for _, it := range m.items {
				if it.Key == k && !it.sep() {
					return m, m.run(it, st)
				}
			}
		}
	}
	return m, nil
}

func (m *menu) move(d int) {
	for i := m.cursor + d; i >= 0 && i < len(m.items); i += d {
		if !m.items[i].sep() {
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
		if it.sep() {
			lines = append(lines, ui.ListLine{Text: ui.Paint(strings.Repeat(ui.G.H, iw), p.Line, p.Surface, false), Item: -1})
			continue
		}
		row := ui.RowLine(iw, i == m.cursor, p.Surface, func(bg color.Color, w int) string {
			fg := p.Text
			if it.Danger {
				fg = p.Coral
			}
			label := capital(it.Label)
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

// footer labels are lowercase; a menu line starts with a capital
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m *menu) Actions(*core.State) []core.Action {
	n := 0
	for _, it := range m.items {
		if !it.sep() {
			n++
		}
	}
	var acts []core.Action
	if n > 1 {
		acts = append(acts, core.Action{Key: "↑↓", Label: "move"})
	}
	return append(acts, core.Action{Key: "enter", Label: "run"}, core.Action{Key: "esc", Label: "close"})
}

func (m *menu) Animating(*core.State) bool { return false }
