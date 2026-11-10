package gate

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var sides = []string{"This computer", "Account"}

// sign-in stays unfinished until every clash is settled, so Esc never closes this
type Clashes struct {
	list []api.Clash
	keep []int
	at   string // the focused stop: a row of chips, then the bar
	top  int    // the first row drawn, so the focused one stays in view
	req  core.Req
}

func NewClashes(list []api.Clash) *Clashes {
	c := &Clashes{list: list, keep: make([]int, len(list)), at: "btn:continue"}
	for i := range c.keep {
		c.keep[i] = 1
	}
	if len(list) > 0 {
		c.at = rowID(0)
	}
	return c
}

func rowID(i int) string { return "chip:" + strconv.Itoa(i) }

// which row has focus, -1 for the bar
func (c *Clashes) row() int {
	if rest, ok := strings.CutPrefix(c.at, "chip:"); ok {
		if i, err := strconv.Atoi(rest); err == nil && i < len(c.list) {
			return i
		}
	}
	return -1
}

func (c *Clashes) stops() []string {
	stops := make([]string, 0, len(c.list)+1)
	for i := range c.list {
		stops = append(stops, rowID(i))
	}
	return append(stops, "btn:continue")
}

func (c *Clashes) busy() bool { return c.req != 0 }

func (c *Clashes) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if c.busy() {
			return c, nil
		}
		return c, c.key(m.String(), st)
	case core.Click:
		if c.busy() {
			return c, nil
		}
		if m.ID == "btn:continue" {
			c.at = m.ID
			return c, c.resolve(st)
		}
		c.pick(m.ID)
	case core.Reconnected:
		c.req = 0
	case core.Reply:
		if m.Req != c.req {
			return c, nil
		}
		c.req = 0
		left, _ := m.Val.([]api.Clash)
		if m.Err != nil {
			c.keepOnly(left)
			return c, core.Fail(m.Err)
		}
		if len(left) > 0 {
			c.keepOnly(left)
			return c, nil
		}
		return c, tea.Batch(core.Close(c), Choose(st, core.NextReq()), core.Say("Signed in"))
	}
	return c, nil
}

// a click on a chip, "chip:row:side", picks that side and focuses the row
func (c *Clashes) pick(id string) {
	rest, ok := strings.CutPrefix(id, "chip:")
	if !ok {
		return
	}
	row, side, _ := strings.Cut(rest, ":")
	if i, err := strconv.Atoi(row); err == nil && i < len(c.list) && (side == "0" || side == "1") {
		c.at, c.keep[i] = rowID(i), int(side[0]-'0')
	}
}

// on a row the arrows pick a side and enter goes on to the next stop; on the bar enter continues
func (c *Clashes) key(k string, st *core.State) tea.Cmd {
	i := c.row()
	switch {
	case i >= 0 && k == "left":
		c.keep[i] = 0
	case i >= 0 && k == "right":
		c.keep[i] = 1
	case i >= 0 && k == "space":
		c.keep[i] = 1 - c.keep[i]
	case i >= 0 && k == "enter":
		c.at = c.stops()[i+1]
	case ui.Pressed(k, c.at):
		return c.resolve(st)
	default:
		ui.Walk(c.stops(), &c.at, k)
	}
	return nil
}

// the rows still open after a partial resolve, with the choices already made
func (c *Clashes) keepOnly(left []api.Clash) {
	if left == nil {
		return
	}
	choice := map[string]int{}
	for i, x := range c.list {
		choice[x.Kind+"\x00"+x.Name] = c.keep[i]
	}
	*c = *NewClashes(left)
	for i, x := range left {
		if k, ok := choice[x.Kind+"\x00"+x.Name]; ok {
			c.keep[i] = k
		}
	}
}

func (c *Clashes) resolve(st *core.State) tea.Cmd {
	c.req = core.NextReq()
	list, keep := c.list, c.keep
	return core.Do(st, c.req, func(ctx context.Context, cl *client.Client) (any, error) {
		left := list
		for i, x := range list {
			if !slices.ContainsFunc(left, func(y api.Clash) bool { return y.Kind == x.Kind && y.Name == x.Name }) {
				continue
			}
			k := "account"
			if keep[i] == 0 {
				k = "local"
			}
			next, err := cl.LoginResolve(ctx, x.Kind, x.Name, k)
			if err != nil {
				return left, err
			}
			left = next
		}
		return left, nil
	})
}

// the bar, also listed by the actions menu; nothing to press while it saves
func (c *Clashes) Buttons(*core.State) []ui.Btn {
	if c.busy() {
		return nil
	}
	return []ui.Btn{{ID: "continue", Label: "Continue", Primary: true}}
}

func (c *Clashes) window(st *core.State, maxW, maxH int) *ui.Window {
	p := ui.P()
	btn := ui.Btn{ID: "continue", Label: "Continue", Primary: true}
	if c.busy() {
		btn.Label = ui.Spin(st.T) + " Saving"
	}
	// a focused row lights no button, so only one thing looks focused
	win := &ui.Window{W: min(76, maxW), Title: "Keep which copy?", Sub: "These names are on this computer and in your account.",
		Buttons: []ui.Btn{btn}, Active: -1}
	if c.row() < 0 {
		win.Active = 0
	}
	n := len(c.list)
	win.Fit(n, maxH)
	room := min(n, win.Room(maxH))
	if i := c.row(); i >= 0 {
		c.top = min(c.top, i)
		c.top = max(c.top, i-room+1)
	}
	c.top = max(0, min(c.top, n-room))
	if room < n {
		win.Corner = ui.L(p.Window).T(fmt.Sprintf("%d-%d of %d", c.top+1, c.top+room, n), p.Muted).String()
	}
	body := ui.NewBox()
	cw := win.Inner()
	for y := range room {
		i := c.top + y
		x := c.list[i]
		kind := "drive"
		if x.Kind == "service" {
			kind = "account"
		}
		label := ui.L(p.Window).B(x.Name, p.Text).Sp(1).T(kind, p.Faint).Pad(cw - 34)
		body.Put(0, y, label)
		body.Add(cw-33, y, ui.Chips(rowID(i), sides, c.keep[i], c.row() == i, p.Window))
	}
	win.Body = body
	return win
}

func (c *Clashes) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return c.window(st, maxW, maxH).Layer()
}

func (c *Clashes) Actions(*core.State) []core.Action {
	if c.busy() {
		return nil
	}
	move := core.Action{Key: "tab", Label: "move"}
	if len(c.list) > 1 {
		move.Key = "↑↓"
	}
	if c.row() < 0 {
		return []core.Action{{Key: "enter", Label: "continue", Accent: true}, move}
	}
	return []core.Action{{Key: "enter", Label: "next"}, move, {Key: "← →", Label: "choose"}, {Key: "space", Label: "switch"}}
}

func (c *Clashes) Animating(*core.State) bool { return c.busy() && !ui.Reduced() }
