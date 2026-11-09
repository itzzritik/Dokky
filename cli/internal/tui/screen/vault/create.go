package vault

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// a new master password typed twice, then its recovery key
type create struct {
	np         newPassword
	focus      int // 0 password, 1 again
	reveal     bool
	err        string
	req        core.Req
	title, sub string
	toast      string // said once the key is saved
}

func newCreate(title, sub, toast string) *create {
	return &create{np: makeNew("Password"), title: title, sub: sub, toast: toast}
}

func (c *create) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
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
			if code(m.Err) == api.ErrWeakPassword {
				c.focus = 0
			}
			return nil
		}
		key, _ := m.Val.(string)
		c.np.wipe()
		f.swap(newShowKey(key, c.toast))
		return nil
	case core.Click:
		switch m.ID {
		case "field:0", "field:1":
			c.focus = int(m.ID[6] - '0')
		case "btn:create":
			return c.submit(st)
		}
	case tea.KeyPressMsg, tea.PasteMsg:
		if c.req != 0 {
			return nil
		}
		return c.key(msg, f, st)
	}
	return nil
}

func (c *create) key(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	k := ""
	if m, ok := msg.(tea.KeyPressMsg); ok {
		k = m.String()
	}
	switch {
	case k == "esc":
		return f.back()
	case k == "ctrl+r":
		c.reveal = !c.reveal
		c.np.reveal(c.reveal)
	case k == "enter" && c.focus == 0 && c.np.again.Len() == 0:
		c.focus = 1
	case k == "enter":
		return c.submit(st)
	case move(k, &c.focus, 2):
	default:
		field := c.np.pw
		if c.focus == 1 {
			field = c.np.again
		}
		if field.Type(msg) {
			c.err = ""
			c.np.rescore()
		}
	}
	return nil
}

func (c *create) submit(st *core.State) tea.Cmd {
	if at, msg := c.np.problem(); at >= 0 {
		c.focus, c.err = at, msg
		return nil
	}
	pw := c.np.pw.String()
	c.req, c.err = core.NextReq(), ""
	return core.Do(st, c.req, func(ctx context.Context, cl *client.Client) (any, error) { return cl.VaultSetup(ctx, pw) })
}

func (c *create) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = c.title
	sub(win, c.sub, c.err)
	win.Buttons = []ui.Btn{{ID: "create", Label: busyLabel(st, c.req != 0, "Create password", "Creating"), Primary: true}}
	win.Active = 0
	cw, bg := win.Inner(), p.Surface
	room := win.Room(maxH)
	g := gap(room, 2, 3)
	body := ui.NewBox()
	body.Add(0, 0, c.np.pw.View("field:0", cw, c.focus == 0 && c.req == 0, bg))
	body.Add(0, g, c.np.again.View("field:1", cw, c.focus == 1 && c.req == 0, bg))
	meter := c.np.rows(cw, bg, room-2*g)
	body.Lines(0, 2*g, meter)
	rows := 2*g + len(meter)
	note := para(win, "dokky never sees it, and it can't be reset without your recovery key.", p.Faint)
	if rows+1+len(note) <= room {
		body.Lines(0, rows+1, note)
		rows += 1 + len(note)
	}
	win.Body = body
	return rows
}

func (c *create) actions(*core.State) []core.Action {
	if c.req != 0 {
		return nil
	}
	next := core.Action{Key: "enter", Label: "create"}
	if c.focus == 0 && c.np.again.Len() == 0 {
		next.Label = "next"
	}
	return []core.Action{next, {Key: "tab", Label: "switch"}, revealAction(c.reveal)}
}

func (c *create) typing() bool               { return true }
func (c *create) holding() bool              { return c.req != 0 }
func (c *create) animating(*core.State) bool { return c.req != 0 && !ui.Reduced() }
func (c *create) wipe()                      { c.np.wipe() }
