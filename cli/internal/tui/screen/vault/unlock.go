package vault

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

// Forged's unlock card: system auth starts by itself, esc falls back to the master password, tab asks again
type unlock struct {
	force  bool // the view was locked, so a live session is not enough
	pw     *ui.Secret
	at     string // field:pw or link:forgot
	reveal bool
	err    string
	req    core.Req
	system bool // the call in flight waits for Touch ID or Windows Hello
	cancel context.CancelFunc
	done   bool // unlocked; the screen waits for the app to take it down
}

func newUnlock(force bool) *unlock {
	return &unlock{force: force, pw: ui.NewSecret("", "", vault.MaxPassword), at: "field:pw"}
}

func (u *unlock) busy() bool { return u.req != 0 || u.done }

// start asks for system auth at once when this computer offers it, as Forged's lock screen does
func (u *unlock) start(st *core.State) tea.Cmd {
	if u.busy() || !offered(st) {
		return nil
	}
	return u.ask(st, api.AuthSystem)
}

func (u *unlock) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Reconnected:
		u.stop()
		u.done = false
	case core.Reply:
		if m.Req != u.req || u.req == 0 {
			return nil
		}
		system := u.system
		u.stop()
		if m.Err != nil {
			u.err, u.at = Explain(m.Err, st), "field:pw"
			if system && code(m.Err) == api.ErrCancelled {
				u.err = ""
			}
			if code(m.Err) == api.ErrWrongPassword {
				u.pw.Wipe()
			}
			return nil
		}
		u.pw.Wipe()
		u.done = true
		return f.finish(st)
	case core.Click:
		return u.click(m.ID, f, st)
	case tea.KeyPressMsg, tea.PasteMsg:
		return u.key(msg, f, st)
	}
	return nil
}

func (u *unlock) click(id string, f *Flow, st *core.State) tea.Cmd {
	switch {
	case id == "key:esc":
		return u.key(core.Press("esc"), f, st)
	case id == "key:tab":
		return u.key(core.Press("tab"), f, st)
	case u.busy():
		return nil
	case id == "field:pw" || id == "link:forgot":
		u.at = id
	}
	return u.press(id, f, st)
}

func (u *unlock) press(id string, f *Flow, st *core.State) tea.Cmd {
	switch id {
	case "link:forgot":
		f.push(newRecover(false))
	case "btn:unlock":
		return u.submit(st)
	}
	return nil
}

func (u *unlock) key(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	k := ""
	if m, ok := msg.(tea.KeyPressMsg); ok {
		k = m.String()
	}
	switch {
	case k == "esc" && u.system && u.req != 0:
		u.stop()
		u.at = "field:pw"
		return nil
	case u.busy():
		return nil
	case k == "esc":
		return f.back()
	case k == "tab" && offered(st):
		return u.ask(st, api.AuthSystem)
	case k == "ctrl+r":
		u.reveal = !u.reveal
		u.pw.Reveal = u.reveal
	case k == "down" && u.at == "field:pw":
		u.at = "link:forgot"
	case k == "up" && u.at == "link:forgot":
		u.at = "field:pw"
	case k == "enter" && u.at == "link:forgot":
		return u.press(u.at, f, st)
	case k == "enter":
		return u.submit(st)
	case u.at == "field:pw" && u.pw.Type(msg):
		u.err = ""
	}
	return nil
}

// enter on an empty field asks system auth when offered; a typed password is checked
func (u *unlock) submit(st *core.State) tea.Cmd {
	switch {
	case u.pw.Len() > 0:
		return u.ask(st, api.AuthPassword)
	case offered(st):
		return u.ask(st, api.AuthSystem)
	}
	u.err = "Type your master password."
	return nil
}

// opens the control session; over SSH the daemon is told, so it never shows a prompt nobody here can answer
func (u *unlock) ask(st *core.State, method string) tea.Cmd {
	u.stop()
	ctx, cancel := context.WithCancel(context.Background())
	u.req, u.cancel, u.system, u.err = core.NextReq(), cancel, method == api.AuthSystem, ""
	p := api.SessionParams{Method: method, Force: u.force, Remote: st.Deps.Remote}
	if method == api.AuthPassword {
		p.Password = u.pw.String()
	}
	return core.DoCtx(ctx, st, u.req, func(ctx context.Context, c *client.Client) (any, error) {
		return nil, c.SessionOpen(ctx, p)
	})
}

// stops the call in flight, like waiting for Touch ID; a late reply is dropped
func (u *unlock) stop() {
	if u.cancel != nil {
		u.cancel()
	}
	u.req, u.cancel, u.system = 0, nil, false
}

// the line under the title: what unlocking does, or why the password is needed this time
func (u *unlock) prompt(st *core.State) string {
	v := st.Snap.Vault
	switch {
	case v.PasswordDue && v.System != "" && !st.Deps.Remote:
		return "It's been " + strconv.Itoa(v.PasswordDays) + " days. Enter your master password this time."
	case v.State == api.VaultLocked:
		return "Your drives mount once dokky is unlocked."
	case offered(st):
		return "Please authenticate to continue using dokky."
	}
	return "Enter your master password to continue using dokky."
}

func (u *unlock) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = "Unlock dokky"
	win.Sub = u.prompt(st)
	cw, bg := win.Inner(), p.Window
	body := ui.NewBox()
	waiting := u.system && u.req != 0
	if waiting {
		body.Put(0, 0, ui.L(bg).T(upper(systemName(st.Snap.Vault)), p.Muted).Pad(cw))
		body.Put(0, 1, ui.L(bg).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Waiting for confirmation", p.Text).Pad(cw))
	} else {
		body.Put(0, 0, ui.L(bg).T("Master password", p.Muted).Pad(cw))
		body.Add(0, 1, u.pw.View("field:pw", cw, u.at == "field:pw" && !u.busy(), bg))
	}
	y := 2
	if u.err != "" {
		body.Put(0, y, ui.L(bg).T(core.Fit(u.err, cw), p.Coral).Pad(cw))
		y++
	}
	if !waiting && y+2 <= win.Room(maxH) {
		body.Add(0, y+1, link("link:forgot", "Forgot password?", u.at == "link:forgot", bg))
		y += 2
	} else if u.at == "link:forgot" {
		u.at = "field:pw"
	}
	win.Buttons = []ui.Btn{{ID: "unlock", Label: busyLabel(st, u.busy() && !u.system, "Unlock", "Unlocking"), Primary: true}}
	win.Active = 0
	win.Body = body
	return y
}

// Forged's hints: system auth on tab, esc quits; while waiting, esc goes to the password
func (u *unlock) actions(st *core.State) []core.Action {
	switch {
	case u.system && u.req != 0:
		return []core.Action{{Key: "esc", Label: "Use password"}}
	case u.busy():
		return nil
	}
	enter := core.Action{Key: "enter", Label: "Unlock"}
	if u.at == "link:forgot" {
		enter.Label = "Reset password"
	}
	acts := []core.Action{enter}
	if offered(st) {
		acts = append(acts, core.Action{Key: "tab", Label: upper(systemName(st.Snap.Vault))})
	}
	return acts
}

func (u *unlock) typing() bool  { return u.at == "field:pw" }
func (u *unlock) holding() bool { return false }

func (u *unlock) animating(*core.State) bool { return u.busy() && !ui.Reduced() }

func (u *unlock) wipe() {
	u.stop()
	u.pw.Wipe()
}
