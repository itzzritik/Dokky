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

// the current master password, then a new one typed twice; the key and recovery key stay
type change struct {
	cur    *ui.Secret
	np     newPassword
	focus  int // 0 current, 1 new, 2 again
	reveal bool
	err    string
	req    core.Req
}

func newChange() *change {
	return &change{cur: ui.NewSecret("Current", "", vault.MaxPassword), np: makeNew("New")}
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
				c.focus = 0
				c.cur.Wipe()
			case api.ErrWeakPassword:
				c.focus = 1
			}
			return nil
		}
		return tea.Batch(core.Say("Master password changed"), f.finish(st))
	case core.Click:
		if c.req != 0 {
			return nil
		}
		switch m.ID {
		case "field:0", "field:1", "field:2":
			c.focus = int(m.ID[6] - '0')
		case "btn:change":
			return c.submit(st)
		case "btn:cancel":
			return f.back()
		}
	case tea.KeyPressMsg, tea.PasteMsg:
		if c.req != 0 {
			return nil
		}
		k := ""
		if km, ok := msg.(tea.KeyPressMsg); ok {
			k = km.String()
		}
		fs := c.fields()
		switch {
		case k == "esc":
			return f.back()
		case k == "ctrl+r":
			c.reveal = !c.reveal
			c.cur.Reveal = c.reveal
			c.np.reveal(c.reveal)
		case k == "enter" && c.focus < 2 && fs[c.focus].Len() > 0 && fs[c.focus+1].Len() == 0:
			c.focus++
		case k == "enter":
			return c.submit(st)
		case move(k, &c.focus, 3):
		case fs[c.focus].Type(msg):
			c.err = ""
			c.np.rescore()
		}
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
		c.focus, c.err = at, msg
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
	win.Active = 1
	cw, bg := win.Inner(), p.Surface
	idle := c.req == 0
	room := win.Room(maxH)
	g := gap(room, 3, 3)
	body := ui.NewBox()
	for i, fd := range c.fields() {
		body.Add(0, g*i, fd.View("field:"+string(rune('0'+i)), cw, c.focus == i && idle, bg))
	}
	meter := c.np.rows(cw, bg, room-3*g)
	body.Lines(0, 3*g, meter)
	win.Body = body
	return 3*g + len(meter)
}

func (c *change) actions(*core.State) []core.Action {
	if c.req != 0 {
		return nil
	}
	return []core.Action{{Key: "enter", Label: "change"}, {Key: "tab", Label: "switch"}, revealAction(c.reveal)}
}

func (c *change) typing() bool               { return true }
func (c *change) holding() bool              { return c.req != 0 }
func (c *change) animating(*core.State) bool { return c.req != 0 && !ui.Reduced() }

func (c *change) wipe() {
	c.cur.Wipe()
	c.np.wipe()
}
