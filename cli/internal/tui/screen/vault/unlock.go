package vault

import (
	"context"
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

type unlockMode int

const (
	atStart  unlockMode = iota // the daemon is locked: vault.unlock
	onWindow                   // the window lock: vault.authorize, its grant unused
	forGrant                   // a sensitive change: vault.authorize, then the grant is spent
)

// proving it is the person: Touch ID or Windows Hello when offered, the master password always
type unlock struct {
	mode       unlockMode
	pw         *ui.Secret
	focus      int // 0 password, 1 forgot password
	reveal     bool
	err        string
	cool       bool // err is a cooldown, which the live countdown replaces
	req        core.Req
	system     bool // the call in flight waits for Touch ID or Windows Hello
	cancel     context.CancelFunc
	done       bool // unlocked; the screen waits for the app to take it down
	title, sub string
	then       func(*core.State, string) tea.Cmd // forGrant: spends the grant
}

func newUnlock(mode unlockMode, title, sub string) *unlock {
	return &unlock{mode: mode, pw: ui.NewSecret("Password", "", vault.MaxPassword), title: title, sub: sub}
}

func (u *unlock) items() int {
	if u.mode == atStart {
		return 2
	}
	return 1
}

func (u *unlock) busy() bool { return u.req != 0 || u.done }

// too many wrong passwords; the daemon's clock says until when
func coolingFor(st *core.State) time.Duration {
	return max(0, st.Snap.Vault.CooldownUntil.Sub(st.Now))
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
		u.stop()
		if m.Err != nil {
			u.err, u.cool, u.focus = Explain(m.Err, st), code(m.Err) == api.ErrTooManyAttempts, 0
			if code(m.Err) == api.ErrWrongPassword {
				u.pw.Wipe()
			}
			return nil
		}
		u.pw.Wipe()
		if u.mode == forGrant {
			g, _ := m.Val.(string)
			f.wipe()
			return tea.Batch(core.Close(f), u.then(st, g))
		}
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
	if u.busy() {
		return nil
	}
	switch id {
	case "btn:system":
		if offered(st) {
			return u.ask(st, api.AuthSystem)
		}
	case "field:pw":
		u.focus = 0
	case "link:forgot":
		f.push(newRecover(false))
	case "btn:unlock":
		return u.submit(st)
	case "btn:cancel":
		return f.back()
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
		u.focus = 0
		return nil
	case u.busy():
		return nil
	case k == "esc":
		return f.back()
	case k == "ctrl+r":
		u.reveal = !u.reveal
		u.pw.Reveal = u.reveal
	case k == "enter" && u.focus == 1:
		f.push(newRecover(false))
	case k == "enter":
		return u.submit(st)
	case move(k, &u.focus, u.items()):
	case u.focus == 0 && u.pw.Type(msg):
		u.err = ""
	}
	return nil
}

// enter on an empty field asks Touch ID when it is offered; a typed password is checked
func (u *unlock) submit(st *core.State) tea.Cmd {
	switch {
	case coolingFor(st) > 0:
		return nil
	case u.pw.Len() > 0:
		return u.ask(st, api.AuthPassword)
	case offered(st):
		return u.ask(st, api.AuthSystem)
	}
	u.err = "Type your master password."
	return nil
}

// over SSH the daemon is told, so it never shows a prompt nobody here can answer
func (u *unlock) ask(st *core.State, method string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	u.req, u.cancel, u.system, u.err = core.NextReq(), cancel, method == api.AuthSystem, ""
	p := api.UnlockParams{Method: method, Remote: st.Deps.Remote}
	if method == api.AuthPassword {
		p.Password = u.pw.String()
	}
	start := u.mode == atStart
	return core.DoCtx(ctx, st, u.req, func(ctx context.Context, c *client.Client) (any, error) {
		if start {
			return nil, c.VaultUnlock(ctx, p)
		}
		return c.VaultAuthorize(ctx, p)
	})
}

// stops the call in flight, like waiting for Touch ID; a late reply is dropped
func (u *unlock) stop() {
	if u.cancel != nil {
		u.cancel()
	}
	u.req, u.cancel, u.system = 0, nil, false
}

func (u *unlock) view(st *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	v := st.Snap.Vault
	win.Title = u.title
	usual := u.sub
	if v.PasswordDue && v.System != "" && !st.Deps.Remote && v.PasswordDays > 0 {
		usual = "It's been " + strconv.Itoa(v.PasswordDays) + " days. Type your master password this time."
	}
	sub(win, usual, errLine(u.err, u.cool, st))
	cw, bg := win.Inner(), p.Surface
	sys := offered(st)
	u.pw.Hint = ""
	if sys {
		u.pw.Hint = "Or type your master password"
	}
	body := ui.NewBox()
	y := 0
	if sys {
		body.Add(0, 0, u.tile(st, cw, bg))
		y = 4
	}
	body.Add(0, y, u.pw.View("field:pw", cw, u.focus == 0 && !u.system, bg))
	y++
	if u.mode == atStart && y+2 <= win.Room(maxH) {
		body.Add(0, y+1, link("link:forgot", "Forgot password?", u.focus == 1, bg))
		y += 2
	}
	label := "Unlock"
	if u.mode == forGrant {
		label = "Confirm"
		win.Buttons = append(win.Buttons, ui.Btn{ID: "cancel", Label: "Cancel"})
	}
	win.Buttons = append(win.Buttons, ui.Btn{ID: "unlock", Label: busyLabel(st, u.busy() && !u.system, label, "Checking"), Primary: true})
	if u.pw.Len() > 0 || !sys || u.busy() && !u.system {
		win.Active = len(win.Buttons) - 1
	}
	win.Body = body
	return y
}

// the big Touch ID button: lit while enter would press it or while it waits
func (u *unlock) tile(st *core.State, w int, bg color.Color) *lipgloss.Layer {
	p := ui.P()
	name := systemName(st.Snap.Vault)
	fill, fg := p.Cap, p.Text
	if u.system || u.pw.Len() == 0 && u.focus == 0 && !u.busy() {
		fill, fg = p.Accent, p.OnAccent
	}
	l := ui.L(fill)
	switch {
	case u.system:
		l.B(ui.Spin(st.T), fg).Sp(1).B("Waiting for "+name, fg)
	case u.mode == forGrant:
		l.B("Confirm with "+name, fg)
	default:
		l.B("Unlock with "+name, fg)
	}
	pad := max(0, (w-l.Width())/2)
	lines := []string{rim(ui.G.Top, w, fill, bg), ui.L(fill).Sp(pad).Raw(l.String()).Pad(w), rim(ui.G.Bottom, w, fill, bg)}
	return lipgloss.NewLayer(strings.Join(lines, "\n")).ID("btn:system")
}

func (u *unlock) actions(st *core.State) []core.Action {
	switch {
	case u.system && u.req != 0:
		return []core.Action{{Key: "esc", Label: "use password"}}
	case u.busy():
		return nil
	}
	enter := core.Action{Key: "enter", Label: "unlock"}
	switch {
	case u.focus == 1:
		enter.Label = "recover"
	case u.pw.Len() == 0 && offered(st):
		enter.Label = strings.TrimPrefix(systemName(st.Snap.Vault), "your ")
	case u.mode == forGrant:
		enter.Label = "confirm"
	}
	acts := []core.Action{enter}
	if u.items() > 1 {
		acts = append(acts, core.Action{Key: "tab", Label: "switch"})
	}
	return append(acts, revealAction(u.reveal))
}

func (u *unlock) typing() bool  { return u.focus == 0 }
func (u *unlock) holding() bool { return false }

func (u *unlock) animating(st *core.State) bool {
	return coolingFor(st) > 0 || u.busy() && !ui.Reduced()
}

func (u *unlock) wipe() {
	u.stop()
	u.pw.Wipe()
}
