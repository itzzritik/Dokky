package vault

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var createStops = []string{"field:0", "field:1", "btn:create"}

// a new master password typed twice, then its recovery key
type create struct {
	np         newPassword
	at         string // the focused stop
	reveal     bool
	err        string
	req        core.Req
	title, sub string
	toast      string // said once the key is saved
}

func newCreate(title, sub, toast string) *create {
	np := makeNew("", "")
	np.pw.Hint, np.again.Hint = "", ""
	return &create{np: np, at: "field:0", title: title, sub: sub, toast: toast}
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
				c.at = "field:0"
			}
			return nil
		}
		key, _ := m.Val.(string)
		c.np.wipe()
		f.swap(newShowKey(key, c.toast))
		return nil
	case core.Click:
		if c.req != 0 {
			return nil
		}
		switch m.ID {
		case "field:0", "field:1":
			c.at = m.ID
		case "btn:create":
			c.at = m.ID
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
	case ui.Pressed(k, c.at):
		return c.submit(st)
	case k == "enter" && c.at == "field:0" && c.np.again.Len() == 0:
		c.at = "field:1"
	case k == "enter":
		return c.submit(st)
	case ui.Walk(createStops, &c.at, k):
	default:
		field := c.np.pw
		switch c.at {
		case "field:1":
			field = c.np.again
		case "btn:create":
			return nil
		}
		if field.Type(msg) {
			c.err = ""
			c.np.recheck()
		}
	}
	return nil
}

func (c *create) submit(st *core.State) tea.Cmd {
	if at, msg := c.np.problem(); at >= 0 {
		c.at, c.err = fieldID(at), msg
		return nil
	}
	pw := c.np.pw.String()
	c.req, c.err = core.NextReq(), ""
	return core.Do(st, c.req, func(ctx context.Context, cl *client.Client) (any, error) { return cl.VaultSetup(ctx, pw) })
}

func (c *create) view(st *core.State, win *ui.Window, maxH int) int {
	win.Title = c.title
	sub(win, c.sub, c.err)
	win.Buttons = []ui.Btn{{ID: "create", Label: busyLabel(st, c.req != 0, "Create password", "Creating"), Primary: true}}
	win.Active = 0
	focus := fieldAt(c.at)
	if c.req != 0 {
		focus = -1
	}
	p, cw := ui.P(), win.Inner()
	body := ui.NewBox()
	rows := c.np.labelled(body, 0, cw, [2]string{"Master password", "Confirm master password"}, [2]string{"field:0", "field:1"}, focus)
	if rows+3 <= win.Room(maxH) {
		for _, l := range ui.Wrap(warning, cw) {
			rows++
			body.Put(0, rows, ui.L(p.Window).T(l, p.Buoy).Pad(cw))
		}
	}
	win.Body = body
	return rows + 1
}

// Forged warns that a lost password loses the keys; dokky has a recovery key to fall back on
const warning = "If you lose this password, only your recovery key can open your drives."

func (c *create) actions(*core.State) []core.Action {
	if c.req != 0 {
		return nil
	}
	next := core.Action{Key: "enter", Label: "Create"}
	if c.at == "field:0" && c.np.again.Len() == 0 {
		next.Label = "Next"
	}
	return []core.Action{next, {Key: "tab", Label: "Next"}}
}

func (c *create) typing() bool               { return fieldAt(c.at) >= 0 }
func (c *create) holding() bool              { return c.req != 0 }
func (c *create) animating(*core.State) bool { return c.req != 0 && !ui.Reduced() }
func (c *create) wipe()                      { c.np.wipe() }
