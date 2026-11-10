package vault

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

var changeStops = []string{"field:0", "field:1", "field:2", "btn:cancel", "btn:change"}

// the current master password, then a new one typed twice; the key and recovery key stay
type change struct {
	cur    *ui.Secret
	np     newPassword
	at     string // the focused stop
	reveal bool
	err    string
	req    core.Req
}

func newChange() *change {
	return &change{cur: ui.NewSecret("Current", "", vault.MaxPassword), np: makeNew("New", "Confirm"), at: "field:0"}
}

func (c *change) fields() []*ui.Secret { return []*ui.Secret{c.cur, c.np.pw, c.np.again} }

func (c *change) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Reconnected:
		c.req = 0
	case core.Reply:
		if m.Req != c.req || c.req == 0 {
			return nil
		}
		c.req = 0
		if m.Err != nil {
			c.err = Explain(m.Err, st)
			switch code(m.Err) {
			case api.ErrWrongPassword:
				c.at = "field:0"
				c.cur.Wipe()
			case api.ErrWeakPassword:
				c.at = "field:1"
			}
			return nil
		}
		return tea.Batch(core.Say("Master password changed"), f.finish(st))
	case core.Click:
		if c.req != 0 {
			return nil
		}
		if slices.Contains(changeStops, m.ID) {
			c.at = m.ID
		}
		return c.press(m.ID, f, st)
	case tea.KeyPressMsg, tea.PasteMsg:
		if c.req != 0 {
			return nil
		}
		k := ""
		if km, ok := msg.(tea.KeyPressMsg); ok {
			k = km.String()
		}
		fs, i := c.fields(), fieldAt(c.at)
		switch {
		case k == "esc":
			return f.back()
		case k == "ctrl+r":
			c.reveal = !c.reveal
			c.cur.Reveal = c.reveal
			c.np.reveal(c.reveal)
		case ui.Pressed(k, c.at):
			return c.press(c.at, f, st)
		case k == "enter" && i >= 0 && i < 2 && fs[i].Len() > 0 && fs[i+1].Len() == 0:
			c.at = fieldID(i + 1)
		case k == "enter":
			return c.submit(st)
		case ui.Walk(changeStops, &c.at, k):
		case i >= 0 && fs[i].Type(msg):
			c.err = ""
			c.np.recheck()
		}
	}
	return nil
}

func (c *change) press(id string, f *Flow, st *core.State) tea.Cmd {
	switch id {
	case "btn:change":
		return c.submit(st)
	case "btn:cancel":
		return f.back()
	}
	return nil
}

func (c *change) problem() (int, string) {
	if c.cur.Len() == 0 {
		return 0, "Type your current master password."
	}
	if at, msg := c.np.problem(); at >= 0 {
		return at + 1, msg
	}
	a, b := c.cur.Runes(), c.np.pw.Runes()
	defer clear(a)
	defer clear(b)
	if slices.Equal(a, b) {
		return 1, "That is the password you have now."
	}
	return -1, ""
}

func (c *change) submit(st *core.State) tea.Cmd {
	if at, msg := c.problem(); at >= 0 {
		c.at, c.err = fieldID(at), msg
		return nil
	}
	cur, next := c.cur.String(), c.np.pw.String()
	c.req, c.err = core.NextReq(), ""
	return core.Do(st, c.req, func(ctx context.Context, cl *client.Client) (any, error) {
		return nil, cl.VaultChangePassword(ctx, cur, next)
	})
}

func (c *change) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Change master password"
	sub(win, "Your recovery key stays the same.", c.err)
	win.Buttons = []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "change", Label: busyLabel(st, c.req != 0, "Change password", "Changing"), Primary: true}}
	win.Active = lit(win, c.at, "btn:change")
	cw, bg := win.Inner(), p.Window
	idle := c.req == 0
	room := win.Room(maxH)
	g := 1
	if room >= 5 {
		g = 2
	}
	focus := fieldAt(c.at) - 1
	if !idle {
		focus = -1
	}
	body := ui.NewBox()
	body.Add(0, 0, c.cur.View("field:0", cw, c.at == "field:0" && idle, bg))
	rows := g + c.np.place(body, g, cw, room-g, [2]string{"field:1", "field:2"}, focus)
	win.Body = body
	return rows
}

func (c *change) actions(*core.State) []core.Action {
	if c.req != 0 {
		return nil
	}
	enter := core.Action{Key: "enter", Label: "change"}
	if c.at == "btn:cancel" {
		enter.Label = "cancel"
	}
	return []core.Action{enter, moveAction, revealAction(c.reveal)}
}

func (c *change) typing() bool               { return fieldAt(c.at) >= 0 }
func (c *change) holding() bool              { return c.req != 0 }
func (c *change) animating(*core.State) bool { return c.req != 0 && !ui.Reduced() }

func (c *change) wipe() {
	c.cur.Wipe()
	c.np.wipe()
}
