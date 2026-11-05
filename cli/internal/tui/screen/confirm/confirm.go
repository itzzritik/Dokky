package confirm

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// a destructive question; focus starts on the safe button
type Confirm struct {
	Question, Line, Do string
	Toggle             string // optional switch, off by default
	Run                func(st *core.State, toggled bool) tea.Cmd

	on, sure bool
	focus    int // 0 toggle, 1 buttons
}

func Open(c *Confirm) tea.Cmd {
	c.focus = 1
	return core.Open(c)
}

func (c *Confirm) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return c, core.Close(c)
		case "left", "right":
			if c.focus == 1 {
				c.sure = !c.sure
			} else {
				c.on = !c.on
			}
		case "up", "down", "tab", "shift+tab":
			if c.Toggle != "" {
				c.focus = 1 - c.focus
			}
		case "space":
			if c.focus == 0 {
				c.on = !c.on
			}
		case "enter":
			if c.focus == 0 {
				c.focus = 1
				return c, nil
			}
			return c, c.finish(st)
		}
	case core.Click:
		switch m.ID {
		case "btn:cancel":
			return c, core.Close(c)
		case "btn:do":
			c.sure = true
			return c, c.finish(st)
		case "chip:toggle:0", "chip:toggle:1":
			c.focus, c.on = 0, m.ID[len(m.ID)-1] == '1'
		}
	}
	return c, nil
}

func (c *Confirm) finish(st *core.State) tea.Cmd {
	if !c.sure {
		return core.Close(c)
	}
	return tea.Batch(core.Close(c), c.Run(st, c.on))
}

func (c *Confirm) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(64, maxW), H: 12, Title: c.Question, Sub: c.Line}
	body := ui.NewBox()
	y := 0
	if c.Toggle != "" {
		win.H += 2
		lw := ui.Width(c.Toggle) + 2
		body.Put(0, 0, ui.L(p.Surface).T(c.Toggle, p.Muted).Pad(lw))
		on := 0
		if c.on {
			on = 1
		}
		body.Add(lw, 0, ui.Chips("chip:toggle", []string{"No", "Yes"}, on, c.focus == 0, p.Surface))
		y = 2
	}
	body.Add(0, y, ui.Button("btn:cancel", "Cancel", false, false, c.focus == 1 && !c.sure))
	body.Add(ui.ButtonWidth("Cancel")+2, y, ui.Button("btn:do", c.Do, false, true, c.focus == 1 && c.sure))
	win.Body = body
	return win.Layer()
}

func (c *Confirm) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "cancel"}}
}

func (c *Confirm) Animating(*core.State) bool { return false }
