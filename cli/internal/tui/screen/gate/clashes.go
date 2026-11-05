package gate

import (
	"context"
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
	sel  int
	req  core.Req
}

func NewClashes(list []api.Clash) *Clashes {
	c := &Clashes{list: list, keep: make([]int, len(list))}
	for i := range c.keep {
		c.keep[i] = 1
	}
	return c
}

func (c *Clashes) busy() bool { return c.req != 0 }

func (c *Clashes) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	n := len(c.list)
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if c.busy() {
			return c, nil
		}
		switch m.String() {
		case "up":
			c.sel = max(0, c.sel-1)
		case "down":
			c.sel = min(n, c.sel+1)
		case "esc":
			c.sel = 0
		case "left", "right", "space":
			if c.sel < n {
				c.keep[c.sel] = 1 - c.keep[c.sel]
			}
		case "enter":
			if c.sel < n {
				c.sel = n
				return c, nil
			}
			return c, c.resolve(st)
		}
	case core.Click:
		if c.busy() {
			return c, nil
		}
		if m.ID == "btn:continue" {
			return c, c.resolve(st)
		}
		if row, side, ok := strings.Cut(strings.TrimPrefix(m.ID, "chip:"), ":"); ok {
			i, _ := strconv.Atoi(row)
			if i < n {
				c.sel, c.keep[i] = i, int(side[0]-'0')
			}
		}
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

func (c *Clashes) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(76, maxW), H: min(maxH, 12+len(c.list)), Title: "Keep which copy?", Sub: "These names are on this computer and in your account."}
	body := ui.NewBox()
	cw := win.Inner()
	for i, x := range c.list {
		kind := "drive"
		if x.Kind == "service" {
			kind = "account"
		}
		label := ui.L(p.Surface).B(x.Name, p.Text).Sp(1).T(kind, p.Faint).Pad(cw - 34)
		body.Put(0, i, label)
		body.Add(cw-33, i, ui.Chips("chip:"+strconv.Itoa(i), sides, c.keep[i], c.sel == i, p.Surface))
	}
	y := len(c.list) + 1
	label := "Continue"
	if c.busy() {
		label = ui.Spin(st.T) + " Saving"
	}
	body.Add(0, y, ui.Button("btn:continue", label, true, false, c.sel == len(c.list)))
	win.Body = body
	return win.Layer()
}

func (c *Clashes) Actions(*core.State) []core.Action {
	if c.sel == len(c.list) {
		return []core.Action{{Key: "enter", Label: "continue"}, {Key: "esc", Label: "back"}}
	}
	return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "← →", Label: "switch"}, {Key: "enter", Label: "next"}}
}

func (c *Clashes) Animating(*core.State) bool { return c.busy() && !ui.Reduced() }
