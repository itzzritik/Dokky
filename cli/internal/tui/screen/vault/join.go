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

// the account already has a master password, set on another device: type it to use it here
type join struct {
	pw     *ui.Secret
	at     string // the focused stop
	reveal bool
	err    string
	cool   bool
	req    core.Req
	toast  string
}

func newJoin(toast string) *join {
	return &join{pw: ui.NewSecret("Password", "", vault.MaxPassword), at: "field:pw", toast: toast}
}

var joinStops = []string{"field:pw", "link:recover", "btn:join"}

// "For you@example.com. You set it on another device."
func joinSub(st *core.State) string {
	if e := st.Snap.Account.Email; e != "" {
		return "For " + e + ". You set it on another device."
	}
	return "Your account has one. You set it on another device."
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
			j.err, j.cool, j.at = Explain(m.Err, st), code(m.Err) == api.ErrTooManyAttempts, "field:pw"
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
		switch m.ID {
		case "field:pw":
			j.at = m.ID
		case "link:recover":
			j.at = m.ID
			f.push(newRecover(true))
		case "btn:join":
			j.at = m.ID
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
		case (k == "enter" || k == "space") && j.at == "link:recover":
			f.push(newRecover(true))
		case k == "enter" || ui.Pressed(k, j.at):
			return j.submit(st)
		case ui.Walk(joinStops, &j.at, k):
		case j.at == "field:pw" && j.pw.Type(msg):
			j.err = ""
		}
	}
	return nil
}

func (j *join) submit(st *core.State) tea.Cmd {
	if coolingFor(st) > 0 {
		return nil
	}
	if j.pw.Len() == 0 {
		j.at, j.err = "field:pw", "Type your master password."
		return nil
	}
	pw := j.pw.String()
	j.req, j.err = core.NextReq(), ""
	return core.Do(st, j.req, func(ctx context.Context, c *client.Client) (any, error) { return nil, c.VaultJoin(ctx, pw) })
}

func (j *join) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Enter your master password"
	sub(win, joinSub(st), errLine(j.err, j.cool, st))
	win.Buttons = []ui.Btn{{ID: "join", Label: busyLabel(st, j.req != 0, "Continue", "Checking"), Primary: true}}
	if j.at != "link:recover" {
		win.Active = 0
	}
	cw, bg := win.Inner(), p.Window
	body := ui.NewBox()
	body.Add(0, 0, j.pw.View("field:pw", cw, j.at == "field:pw" && j.req == 0, bg))
	rows := 1
	if win.Room(maxH) >= 3 {
		body.Add(0, 2, link("link:recover", "Use recovery key instead", j.at == "link:recover", bg))
		rows = 3
	}
	win.Body = body
	return rows
}

func (j *join) actions(*core.State) []core.Action {
	if j.req != 0 {
		return nil
	}
	enter := core.Action{Key: "enter", Label: "continue"}
	if j.at == "link:recover" {
		enter.Label = "use recovery key"
	}
	return []core.Action{enter, moveAction, revealAction(j.reveal)}
}

func (j *join) typing() bool  { return j.at == "field:pw" }
func (j *join) holding() bool { return j.req != 0 }
func (j *join) animating(st *core.State) bool {
	return coolingFor(st) > 0 || j.req != 0 && !ui.Reduced()
}
func (j *join) wipe() { j.pw.Wipe() }
