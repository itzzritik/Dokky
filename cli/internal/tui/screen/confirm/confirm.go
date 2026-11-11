package confirm

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// a destructive question; focus starts on the safe button
type Confirm struct {
	Question, Line, Do string
	Lines              []string // more to know, under the line
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
		// two buttons, so every move lands on the other one
		case "left", "right", "up", "down":
			c.active = 1 - c.active
		case "enter", "space":
			return c, c.press(st, c.active)
		}
	case core.Click:
		switch m.ID {
		case "btn:cancel":
			c.active = 0
			return c, c.press(st, 0)
		case "btn:do":
			c.active = 1
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

// the bar, also listed by the actions menu
func (c *Confirm) Buttons(*core.State) []ui.Btn {
	return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "do", Label: c.Do, Danger: !c.Positive, Primary: c.Positive}}
}

func (c *Confirm) window(st *core.State, maxW, maxH int) *ui.Window {
	win := &ui.Window{W: min(64, maxW), Title: c.Question, Sub: c.Line, Active: c.active, Buttons: c.Buttons(st), Hints: ui.WindowKeys(c.Actions(st))}
	p, cw := ui.P(), win.Inner()
	body, y := ui.NewBox(), 0
	for _, l := range c.Lines {
		for _, part := range strings.Split(lipgloss.NewStyle().Width(cw).Render(l), "\n") {
			body.Put(0, y, win.Text(strings.TrimRight(part, " "), p.Muted))
			y++
		}
	}
	win.Fit(y, maxH)
	if y > 0 {
		win.Body = body
	}
	return win
}

func (c *Confirm) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return c.window(st, maxW, maxH).Layer()
}

// enter names the focused button, so the footer says what it does now
func (c *Confirm) Actions(*core.State) []core.Action {
	enter := core.Action{Key: "enter", Label: "cancel"}
	if c.active == 1 {
		enter = core.Action{Key: "enter", Label: strings.ToLower(c.Do), Danger: !c.Positive, Accent: c.Positive}
	}
	return []core.Action{enter, {Key: "← →", Label: "choose"}, {Key: "esc", Label: "cancel"}}
}

func (c *Confirm) Animating(*core.State) bool { return false }
