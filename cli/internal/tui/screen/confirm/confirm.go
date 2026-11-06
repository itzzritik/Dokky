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
	Run                func(st *core.State) tea.Cmd
	Positive           bool // nothing is lost: the button is primary and focused

	active int // 0 cancel, 1 do
}

func Open(c *Confirm) tea.Cmd {
	if c.Positive {
		c.active = 1
	}
	return core.Open(c)
}

func (c *Confirm) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return c, core.Close(c)
		case "left", "right":
			c.active = 1 - c.active
		case "enter":
			return c, c.press(st, c.active)
		}
	case core.Click:
		switch m.ID {
		case "btn:cancel":
			return c, c.press(st, 0)
		case "btn:do":
			return c, c.press(st, 1)
		}
	}
	return c, nil
}

func (c *Confirm) press(st *core.State, i int) tea.Cmd {
	if i == 0 {
		return core.Close(c)
	}
	return tea.Batch(core.Close(c), c.Run(st))
}

func (c *Confirm) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	win := &ui.Window{W: min(64, maxW), Title: c.Question, Sub: c.Line, Active: c.active,
		Buttons: []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "do", Label: c.Do, Danger: !c.Positive, Primary: c.Positive}}}
	win.Fit(0, maxH)
	return win.Layer()
}

func (c *Confirm) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "cancel"}}
}

func (c *Confirm) Animating(*core.State) bool { return false }
