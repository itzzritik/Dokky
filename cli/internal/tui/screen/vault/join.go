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

// the account already has a master password, set on another device: type it to use it here, or use the recovery key
type join struct {
	pw    *ui.Secret
	at    string // the focused stop
	err   string
	req   core.Req
	toast string
}

func newJoin(toast string) *join {
	pw := ui.NewSecret("", "", vault.MaxPassword)
	pw.Eye = true
	return &join{pw: pw, at: "field:pw", toast: toast}
}

var joinStops = []string{"field:pw", ui.EyeID("field:pw"), "btn:recover", "btn:join"}

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
			j.err, j.at = Explain(m.Err, st), "field:pw"
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
		if j.req != 0 {
			return nil
		}
		if slices.Contains(joinStops, m.ID) {
			j.at = m.ID
		}
		return j.press(m.ID, f, st)
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
			return f.back(st)
		case ui.Pressed(k, j.at):
			return j.press(j.at, f, st)
		case k == "enter":
			return j.submit(st)
		case ui.Walk(joinStops, &j.at, k):
		case j.at == "field:pw" && j.pw.Type(msg):
			j.err = ""
		}
	}
	return nil
}

func (j *join) press(id string, f *Flow, st *core.State) tea.Cmd {
	switch id {
	case ui.EyeID("field:pw"):
		j.pw.Reveal = !j.pw.Reveal
	case "btn:recover":
		f.push(newRecover(true))
	case "btn:join":
		return j.submit(st)
	}
	return nil
}

func (j *join) submit(st *core.State) tea.Cmd {
	if j.pw.Len() == 0 {
		j.at, j.err = "field:pw", "Type your master password."
		return nil
	}
	pw := j.pw.String()
	j.req, j.err = core.NextReq(), ""
	return core.Do(st, j.req, func(ctx context.Context, c *client.Client) (any, error) { return nil, c.VaultJoin(ctx, pw) })
}

// just the title, the field and two buttons; the line under the title shows only a problem
func (j *join) view(st *core.State, win *ui.Window, maxH int) int {
	win.Title = "Enter your master password"
	sub(win, "", j.err)
	win.Buttons = []ui.Btn{{ID: "recover", Label: "Recovery key"}, {ID: "join", Label: busyLabel(st, j.req != 0, "Continue", "Checking"), Primary: true}}
	win.Active = lit(win, j.at, "btn:join")
	body := ui.NewBox()
	at := j.at
	if j.req != 0 {
		at = ""
	}
	body.Add(0, 0, j.pw.View("field:pw", win.Inner(), at, ui.P().Window))
	win.Body = body
	return 1
}

// esc is the only key: the arrows and enter work the buttons, which say what they do
func (j *join) actions(*core.State) []core.Action { return nil }

func (j *join) typing() bool  { return j.at == "field:pw" }
func (j *join) holding() bool { return j.req != 0 }
func (j *join) animating(st *core.State) bool {
	return j.req != 0 && !ui.Reduced()
}
func (j *join) wipe() { j.pw.Wipe() }
