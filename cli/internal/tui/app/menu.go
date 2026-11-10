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
		global = a.menuGlobals()
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
		case "esc", "?", "m":
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

// Forged's menu: an icon, the label and its key on each row, the bar and tint on the chosen one
func (m *menu) View(_ *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(menuW, maxW), Title: "Actions", Flush: true}
	iw, bg := win.Edge(), p.Window
	icons := 0
	for _, it := range m.items {
		if it.Icon != "" {
			icons = 2 + ui.Width(it.Icon)
		}
	}
	lines := []ui.ListLine{{Text: ui.L(bg).Pad(iw), Item: -1}}
	for i, it := range m.items {
		if it.sep() {
			lines = append(lines, ui.ListLine{Text: ui.L(bg).Sp(2).T(strings.Repeat(ui.G.H, iw-4), p.Line).Pad(iw), Item: -1})
			continue
		}
		on := i == m.cursor
		row := ui.RowLine(iw, on, bg, func(bg color.Color, w int) string {
			fg, ic := p.Text, p.Muted
			switch {
			case it.Danger:
				fg, ic = p.Coral, p.Coral
			case on:
				ic = p.Accent
			}
			l := ui.L(bg)
			if icons > 0 {
				l.Raw(ui.L(bg).T(it.Icon, ic).Pad(icons))
			}
			if on {
				l.B(ui.Capital(it.Label), fg)
			} else {
				l.T(ui.Capital(it.Label), fg)
			}
			return l.Right(w-1, ui.L(bg).B(it.Key, p.Muted))
		})
		lines = append(lines, ui.ListLine{Text: row, Item: i, ID: "menu:" + strconv.Itoa(i)})
	}
	lines = append(lines, ui.ListLine{Text: ui.L(bg).Pad(iw), Item: -1})
	rows := min(len(lines), win.Room(maxH))
	body := ui.NewBox()
	ui.List(body, 0, rows, &m.top, m.cursor, lines)
	win.Body = body
	win.Fit(rows, maxH)
	return win.Layer()
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
		acts = append(acts, core.Action{Key: "↑↓", Label: "Move"})
	}
	return append(acts, core.Action{Key: "enter", Label: "Run"}, core.Action{Key: "esc", Label: "Close"})
}

func (m *menu) Animating(*core.State) bool { return false }
