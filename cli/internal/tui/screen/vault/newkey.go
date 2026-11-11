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

// the master password, for a recovery key that replaces the old one
type askKey struct {
	pw  *ui.Secret
	at  string // the focused stop
	err string
	req core.Req
}

func newAskKey() *askKey {
	pw := ui.NewSecret("Password", "Your master password", vault.MaxPassword)
	pw.Eye = true
	return &askKey{pw: pw, at: "field:pw"}
}

var askKeyStops = []string{"field:pw", ui.EyeID("field:pw"), "btn:cancel", "btn:create"}

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
			a.err = Explain(m.Err, st)
			if code(m.Err) == api.ErrWrongPassword {
				a.pw.Wipe()
				a.at = "field:pw"
			}
			return nil
		}
		key, _ := m.Val.(string)
		f.swap(newShowKey(key, "New recovery key saved"))
	case core.Click:
		if a.req != 0 {
			return nil
		}
		if slices.Contains(askKeyStops, m.ID) {
			a.at = m.ID
		}
		return a.press(m.ID, f, st)
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
			return f.back(st)
		case ui.Pressed(k, a.at):
			return a.press(a.at, f, st)
		case k == "enter":
			return a.submit(st)
		case ui.Walk(askKeyStops, &a.at, k):
		case a.at == "field:pw" && a.pw.Type(msg):
			a.err = ""
		}
	}
	return nil
}

func (a *askKey) press(id string, f *Flow, st *core.State) tea.Cmd {
	switch id {
	case ui.EyeID("field:pw"):
		a.pw.Reveal = !a.pw.Reveal
	case "btn:create":
		return a.submit(st)
	case "btn:cancel":
		return f.back(st)
	}
	return nil
}

func (a *askKey) submit(st *core.State) tea.Cmd {
	if a.pw.Len() == 0 {
		a.at, a.err = "field:pw", "Type your master password."
		return nil
	}
	pw := a.pw.String()
	a.req, a.err = core.NextReq(), ""
	return core.Do(st, a.req, func(ctx context.Context, c *client.Client) (any, error) { return c.VaultNewRecoveryKey(ctx, pw) })
}

func (a *askKey) view(st *core.State, win *ui.Window, _ int) int {
	p := ui.P()
	win.Title = "New recovery key"
	sub(win, "Your current recovery key stops working.", a.err)
	win.Buttons = []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "create", Label: busyLabel(st, a.req != 0, "Create key", "Creating"), Primary: true}}
	win.Active = lit(win, a.at, "btn:create")
	body := ui.NewBox()
	at := a.at
	if a.req != 0 {
		at = ""
	}
	body.Add(0, 0, a.pw.View("field:pw", win.Inner(), at, p.Window))
	win.Body = body
	return 1
}

func (a *askKey) actions(*core.State) []core.Action {
	if a.req != 0 {
		return nil
	}
	enter := core.Action{Key: "enter", Label: "create key"}
	if a.at == "btn:cancel" {
		enter.Label = "cancel"
	}
	return []core.Action{enter, moveAction}
}

func (a *askKey) typing() bool  { return a.at == "field:pw" }
func (a *askKey) holding() bool { return a.req != 0 }
func (a *askKey) animating(st *core.State) bool {
	return a.req != 0 && !ui.Reduced()
}
func (a *askKey) wipe() { a.pw.Wipe() }
