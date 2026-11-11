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

// the account already has a master password, set on another device: type it to use it here, or ctrl+o for the recovery key
type join struct {
	pw     *ui.Secret
	reveal bool
	err    string
	req    core.Req
	toast  string
}

func newJoin(toast string) *join {
	return &join{pw: ui.NewSecret("", "", vault.MaxPassword), toast: toast}
}

func (j *join) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Reconnected:
		j.req = 0
	case core.Reply:
		if m.Req != j.req || j.req == 0 {
			return nil
		}
		j.req = 0
		if m.Err != nil {
			j.err = Explain(m.Err, st)
			if code(m.Err) == api.ErrWrongPassword {
				j.pw.Wipe()
			}
			return nil
		}
		var said tea.Cmd
		if j.toast != "" {
			said = core.Say(j.toast)
		}
		return tea.Batch(said, f.finish(st))
	case core.Click:
		if j.req == 0 && m.ID == "btn:join" {
			return j.submit(st)
		}
	case tea.KeyPressMsg, tea.PasteMsg:
		if j.req != 0 {
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
			j.reveal = !j.reveal
			j.pw.Reveal = j.reveal
		case k == recoveryKey.Key:
			f.push(newRecover(true))
		case k == "enter":
			return j.submit(st)
		case j.pw.Type(msg):
			j.err = ""
		}
	}
	return nil
}

func (j *join) submit(st *core.State) tea.Cmd {
	if j.pw.Len() == 0 {
		j.err = "Type your master password."
		return nil
	}
	pw := j.pw.String()
	j.req, j.err = core.NextReq(), ""
	return core.Do(st, j.req, func(ctx context.Context, c *client.Client) (any, error) { return nil, c.VaultJoin(ctx, pw) })
}

// just the title, the field and the button; the line under the title shows only a problem
func (j *join) view(st *core.State, win *ui.Window, maxH int) int {
	win.Title = "Enter your master password"
	sub(win, "", j.err)
	win.Buttons = []ui.Btn{{ID: "join", Label: busyLabel(st, j.req != 0, "Continue", "Checking"), Primary: true}}
	win.Active = 0
	body := ui.NewBox()
	body.Add(0, 0, j.pw.View("field:pw", win.Inner(), j.req == 0, ui.P().Window))
	win.Body = body
	return 1
}

func (j *join) actions(*core.State) []core.Action {
	if j.req != 0 {
		return nil
	}
	return []core.Action{{Key: "enter", Label: "Continue"}, recoveryKey}
}

// the field takes every letter and the arrows are for moving, so the recovery key has its own chord
var recoveryKey = core.Action{Key: "ctrl+o", Label: "Recovery key"}

func (j *join) typing() bool  { return true }
func (j *join) holding() bool { return j.req != 0 }
func (j *join) animating(st *core.State) bool {
	return j.req != 0 && !ui.Reduced()
}
func (j *join) wipe() { j.pw.Wipe() }
