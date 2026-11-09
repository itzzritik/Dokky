package vault

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// the master password, for a recovery key that replaces the old one
type askKey struct {
	pw     *ui.Secret
	reveal bool
	err    string
	cool   bool
	req    core.Req
}

func newAskKey() *askKey {
	return &askKey{pw: ui.NewSecret("Password", "Your master password", vault.MaxPassword)}
}

func (a *askKey) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Reconnected:
		a.req = 0
	case core.Reply:
		if m.Req != a.req || a.req == 0 {
			return nil
		}
		a.req = 0
		if m.Err != nil {
			a.err, a.cool = Explain(m.Err, st), code(m.Err) == api.ErrTooManyAttempts
			if code(m.Err) == api.ErrWrongPassword {
				a.pw.Wipe()
			}
			return nil
		}
		key, _ := m.Val.(string)
		f.swap(newShowKey(key, "New recovery key saved"))
	case core.Click:
		switch {
		case a.req != 0:
		case m.ID == "btn:create":
			return a.submit(st)
		case m.ID == "btn:cancel":
			return f.back()
		}
	case tea.KeyPressMsg, tea.PasteMsg:
		if a.req != 0 {
			return nil
		}
		k := ""
		if km, ok := msg.(tea.KeyPressMsg); ok {
			k = km.String()
		}
		switch {
		case k == "esc":
			return f.back()
		case k == "ctrl+r":
			a.reveal = !a.reveal
			a.pw.Reveal = a.reveal
		case k == "enter":
			return a.submit(st)
		case a.pw.Type(msg):
			a.err = ""
		}
	}
	return nil
}

func (a *askKey) submit(st *core.State) tea.Cmd {
	if coolingFor(st) > 0 {
		return nil
	}
	if a.pw.Len() == 0 {
		a.err = "Type your master password."
		return nil
	}
	pw := a.pw.String()
	a.req, a.err = core.NextReq(), ""
	return core.Do(st, a.req, func(ctx context.Context, c *client.Client) (any, error) { return c.VaultNewRecoveryKey(ctx, pw) })
}

func (a *askKey) view(st *core.State, win *ui.Window, _ int) int {
	p := ui.P()
	win.Title = "New recovery key"
	sub(win, "Your current recovery key stops working.", errLine(a.err, a.cool, st))
	win.Buttons = []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "create", Label: busyLabel(st, a.req != 0, "Create key", "Creating"), Primary: true}}
	win.Active = 1
	body := ui.NewBox()
	body.Add(0, 0, a.pw.View("field:pw", win.Inner(), a.req == 0, p.Surface))
	win.Body = body
	return 1
}

func (a *askKey) actions(*core.State) []core.Action {
	if a.req != 0 {
		return nil
	}
	return []core.Action{{Key: "enter", Label: "create key"}, revealAction(a.reveal)}
}

func (a *askKey) typing() bool  { return true }
func (a *askKey) holding() bool { return a.req != 0 }
func (a *askKey) animating(st *core.State) bool {
	return coolingFor(st) > 0 || a.req != 0 && !ui.Reduced()
}
func (a *askKey) wipe() { a.pw.Wipe() }
