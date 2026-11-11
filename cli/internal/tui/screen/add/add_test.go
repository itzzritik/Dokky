package add

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

var (
	webdav = api.Provider{Type: "webdav", Name: "WebDAV", Desc: "A WebDAV server."}
	sftp   = api.Provider{Type: "sftp", Name: "SFTP", Desc: "A server over SSH."}
	gdrive = api.Provider{Type: "drive", Name: "Google Drive", Desc: "Your Google Drive."}
)

func newState() *core.State {
	return &core.State{Now: time.Now(), Connected: true, W: 100, H: 40, Catalog: []api.Provider{webdav, sftp, gdrive}}
}

func key(a *Add, st *core.State, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = a.Update(core.Press(k), st)
	}
	return cmd
}

func typed(a *Add, st *core.State, s string) {
	for _, r := range s {
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)}, st)
	}
}

func click(a *Add, st *core.State, id string) tea.Cmd {
	_, cmd := a.Update(core.Click{ID: id}, st)
	return cmd
}

// the messages a command sends, for commands that only send messages
func sent(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, sent(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func active(a *Add, st *core.State) int { return a.window(st, 100, 40).Active }

// what enter does in the footer right now, "" when it does nothing
func enter(acts []core.Action) string {
	for _, act := range acts {
		if act.Key == "enter" {
			return act.Label
		}
	}
	return ""
}

// tab from where focus is round to it again, every stop on the way
func walk(t *testing.T, a *Add, st *core.State) []string {
	t.Helper()
	start := a.focused(st)
	out, left := []string{start}, false
	// down may move a list's selection before it moves focus
	for range 40 {
		key(a, st, "down")
		at := a.focused(st)
		if at == start && left {
			return out
		}
		if at != start && !slices.Contains(out, at) {
			left = true
			out = append(out, at)
		}
	}
	t.Fatal("down never came back round")
	return nil
}

// tab reaches every button in the bar, after every stop above it
func requireAllButtons(t *testing.T, a *Add, st *core.State) {
	t.Helper()
	stops := walk(t, a, st)
	require.ElementsMatch(t, a.stops(st), stops)
	for _, b := range a.bar(st) {
		require.Contains(t, stops, "btn:"+b.ID)
	}
}

func signInForm(t *testing.T, st *core.State, p api.Provider, opts []api.Option) *Add {
	t.Helper()
	a := newAdd()
	a.choose(st, p, nil)
	a.Update(core.Reply{Req: a.sign.loadReq, Val: loaded{opts: opts}}, st)
	return a
}

var webdavOpts = []api.Option{{Name: "url", Required: true}, {Name: "user", Required: true}, {Name: "pass", Secret: true}}

func TestChooseWalksTheSearchAndItsButtons(t *testing.T) {
	st := newState()
	a := newAdd()
	require.True(t, a.Capturing(), "the search takes typing")
	require.Equal(t, 1, active(a, st), "Next lights while the search has focus")
	require.Equal(t, "choose", enter(a.Actions(st)))
	requireAllButtons(t, a, st)

	toButtons := func() {
		for i := 0; a.pick.at == searchID && i < 99; i++ {
			key(a, st, "down")
		}
	}
	toButtons()
	require.Equal(t, "btn:cancel", a.pick.at, "down past the last service reaches the buttons, never tab")
	require.False(t, a.Capturing(), "m works on a button")
	require.Equal(t, 0, active(a, st))
	require.Equal(t, "cancel", enter(a.Actions(st)))
	key(a, st, "right")
	require.Equal(t, "btn:next", a.pick.at)
	key(a, st, "right")
	require.Equal(t, "btn:next", a.pick.at, "the bar does not wrap")
	key(a, st, "up", "up")
	require.Equal(t, searchID, a.pick.at, "up goes back to the search")
	toButtons()
	require.Contains(t, sent(key(a, st, "enter")), tea.Msg(core.CloseWindow{Screen: a}), "enter on Cancel closes")
}

func TestChooseNextAndClicks(t *testing.T) {
	st := newState()
	a := newAdd()
	key(a, st, "down")
	a.pick.at = "btn:next"
	require.NotNil(t, key(a, st, "enter"))
	require.Equal(t, signin, a.step)
	require.Equal(t, sftp.Type, a.prov.Type, "Next takes the highlighted service")

	a = newAdd()
	key(a, st, "down")
	click(a, st, searchID)
	require.Equal(t, searchID, a.pick.at, "a click moves focus to what it hit")
	key(a, st, "down")
	require.NotNil(t, click(a, st, "row:2"))
	require.Equal(t, gdrive.Type, a.prov.Type)
}

func TestChooseEscClearsTheSearchFirst(t *testing.T) {
	st := newState()
	a := newAdd()
	typed(a, st, "zzz")
	require.Equal(t, "clear", a.Actions(st)[len(a.Actions(st))-1].Label)
	key(a, st, "down")
	require.Nil(t, key(a, st, "enter"), "nothing matches, so nothing is chosen")
	require.Nil(t, key(a, st, "esc"))
	require.Empty(t, a.pick.search.Value())
	require.Equal(t, choose, a.step)
	require.Contains(t, sent(key(a, st, "esc")), tea.Msg(core.CloseWindow{Screen: a}))
}

func TestSignInEnterGoesToTheNextEmptyField(t *testing.T) {
	st := newState()
	a := signInForm(t, st, webdav, webdavOpts)
	require.Equal(t, "field:0", a.sign.at, "focus starts on the first field")
	require.True(t, a.Capturing())
	require.Equal(t, 1, active(a, st), "Sign in lights while a field has focus")
	requireAllButtons(t, a, st)

	typed(a, st, "https://nas")
	require.Equal(t, "next", enter(a.Actions(st)))
	key(a, st, "enter")
	require.Equal(t, "field:1", a.sign.at, "enter goes on to Username")
	require.Empty(t, a.sign.err, "and says nothing is missing")

	typed(a, st, "me")
	require.Equal(t, "sign in", enter(a.Actions(st)))
	require.NotNil(t, key(a, st, "enter"))
	require.NotZero(t, a.sign.req, "the last one filled signs in")
	require.False(t, a.Capturing(), "nothing is typed while it checks")
	require.Equal(t, []core.Action{{Key: "esc", Label: "back"}}, a.Actions(st))
	require.Equal(t, 1, active(a, st))
	require.Nil(t, key(a, st, "down"), "keys wait for the check")

	a.Update(core.Reply{Req: a.sign.req, Err: errors.New("no route to host")}, st)
	require.Equal(t, "field:1", a.sign.at, "a failed check keeps focus")
	require.NotEmpty(t, a.sign.err)
}

func TestSignInEnterSubmitsOnlyWhenNothingIsLeft(t *testing.T) {
	st := newState()
	a := signInForm(t, st, webdav, webdavOpts)
	key(a, st, "down")
	typed(a, st, "me")
	key(a, st, "up")
	key(a, st, "enter")
	require.Equal(t, "Fill in URL.", a.sign.err, "the field in hand is the only one left")
	require.Equal(t, "field:0", a.sign.at)
	require.Zero(t, a.sign.req)
}

func TestSignInBackButton(t *testing.T) {
	st := newState()
	a := signInForm(t, st, webdav, webdavOpts)
	key(a, st, "up", "up")
	require.Equal(t, "btn:back", a.sign.at)
	require.Equal(t, "back", enter(a.Actions(st)))
	key(a, st, "enter")
	require.Equal(t, choose, a.step)
	require.Equal(t, searchID, a.pick.at)
}

func TestChipRowsSlideWithoutWrapping(t *testing.T) {
	st := newState()
	opts := []api.Option{{Name: "host", Required: true}, {Name: "user", Required: true}, {Name: "port"}, {Name: "pass", Secret: true}, {Name: "key_file"}, {Name: "key_file_pass", Secret: true}}
	a := signInForm(t, st, sftp, opts)
	require.Equal(t, []string{"field:0", "field:1", "field:2", "chip:3", "field:4"}, a.sign.stops())

	key(a, st, "down", "down", "down")
	require.Equal(t, "chip:3", a.sign.at)
	require.False(t, a.Capturing())
	require.Contains(t, a.Actions(st), core.Action{Key: "← →", Label: "choose"})
	auth := a.sign.visible()[3]
	key(a, st, "left")
	require.Equal(t, 0, auth.sel, "left stops at the first")
	key(a, st, "right")
	require.Equal(t, 1, auth.sel)
	key(a, st, "right")
	require.Equal(t, 1, auth.sel, "right stops at the last")
	key(a, st, "space")
	require.Equal(t, 0, auth.sel, "space toggles")
	key(a, st, "space")
	require.Equal(t, 1, auth.sel)
	require.Equal(t, "key_file", a.sign.visible()[4].opt.Name, "an SSH key swaps the password field out")

	click(a, st, "field:0")
	click(a, st, "chip:3:0")
	require.Equal(t, "chip:3", a.sign.at, "a chip click focuses its row")
	require.Equal(t, 0, auth.sel)
}

func oauthWaiting(t *testing.T, st *core.State) *Add {
	t.Helper()
	a := signInForm(t, st, gdrive, []api.Option{{Name: "token"}})
	require.True(t, a.sign.waiting(), "a browser sign-in starts by itself")
	require.Equal(t, []ui.Btn{{ID: "cancel", Label: "Cancel"}}, a.bar(st))
	a.Update(authTick{req: a.sign.req}, st)
	a.Update(core.Reply{Req: a.sign.authPoll, Val: "https://accounts.example.com/auth"}, st)
	return a
}

func TestOAuthWaitHasARealBar(t *testing.T) {
	st := newState()
	a := oauthWaiting(t, st)
	require.Equal(t, "btn:open", a.sign.at, "focus lands on Open browser once the link is there")
	win := a.window(st, 100, 40)
	require.Equal(t, []string{"Cancel", "Copy link", "Open browser"}, []string{win.Buttons[0].Label, win.Buttons[1].Label, win.Buttons[2].Label})
	require.Equal(t, 2, win.Active)
	require.False(t, a.Capturing())
	require.Equal(t, "open browser", enter(a.Actions(st)))
	require.Contains(t, a.Actions(st), core.Action{Key: "← →", Label: "switch"})
	require.Equal(t, "cancel", a.Actions(st)[len(a.Actions(st))-1].Label)
	requireAllButtons(t, a, st)

	key(a, st, "left")
	require.Equal(t, "btn:copy", a.sign.at)
	require.Equal(t, 1, active(a, st))
	require.Contains(t, sent(key(a, st, "enter")), tea.Msg(core.Toast{Text: "Link copied"}))
	require.NotNil(t, key(a, st, "o"), "o still opens the browser")
	require.Equal(t, "btn:open", a.sign.at)
	require.Contains(t, sent(key(a, st, "c")), tea.Msg(core.Toast{Text: "Link copied"}), "c still copies")
	require.Equal(t, "btn:copy", a.sign.at)

	key(a, st, "left")
	require.Equal(t, "btn:cancel", a.sign.at)
	key(a, st, "enter")
	require.Equal(t, choose, a.step, "Cancel stops waiting and goes back")
	require.Zero(t, a.sign.req)
}

func TestDetailsGoesBackToAFinishedSignIn(t *testing.T) {
	st := newState()
	a := oauthWaiting(t, st)
	a.Update(core.Reply{Req: a.sign.req, Val: client.ConfigureReply{Step: api.Step{Done: true, Service: map[string]string{"token": "t"}}}}, st)
	require.Equal(t, name, a.step)
	require.Equal(t, "field:0", a.named.at)
	typed(a, st, " work")

	require.Equal(t, "back", a.Actions(st)[len(a.Actions(st))-1].Label)
	key(a, st, "esc")
	require.Equal(t, signin, a.step, "esc goes back one step")
	require.True(t, a.sign.signedIn(), "and keeps the sign-in")
	require.Equal(t, "btn:next", a.sign.at)
	require.Equal(t, "next", enter(a.Actions(st)))
	requireAllButtons(t, a, st)

	require.Nil(t, key(a, st, "enter"), "Next goes on without signing in again")
	require.Equal(t, name, a.step)
	require.Equal(t, "Google Drive work", a.named.name.Value(), "what was typed stays")
}

func details(t *testing.T, st *core.State) *Add {
	t.Helper()
	a := newAdd()
	a.pick.accounts = []api.AccountRef{{ID: "s1", Name: "Home NAS", Type: webdav.Type}}
	key(a, st, "enter")
	require.Equal(t, name, a.step, "an account already signed in skips sign in")
	return a
}

func TestDetailsStops(t *testing.T) {
	st := newState()
	a := details(t, st)
	require.Equal(t, "field:0", a.named.at)
	require.True(t, a.Capturing())
	require.Equal(t, 1, active(a, st), "Add drive lights while a field has focus")
	requireAllButtons(t, a, st)

	key(a, st, "down", "down")
	require.Equal(t, "chip:login", a.named.at)
	require.False(t, a.Capturing())
	require.Equal(t, "add drive", enter(a.Actions(st)))
	require.Contains(t, a.Actions(st), core.Action{Key: "← →", Label: "choose"})
	key(a, st, "left")
	require.Equal(t, 0, a.named.stayOff, "left stops at Mount")
	key(a, st, "right", "right")
	require.Equal(t, 1, a.named.stayOff, "right stops at Stay off")
	key(a, st, "space")
	require.Equal(t, 0, a.named.stayOff, "space toggles")

	key(a, st, "down")
	require.Equal(t, "btn:back", a.named.at)
	require.Equal(t, 0, active(a, st))
	click(a, st, "chip:login:1")
	require.Equal(t, "chip:login", a.named.at, "a click moves focus to what it hit")
	require.Equal(t, 1, a.named.stayOff)
	click(a, st, "field:1")
	require.Equal(t, "field:1", a.named.at)
}

func TestDetailsEnterGoesToTheName(t *testing.T) {
	st := newState()
	a := details(t, st)
	a.named.name.Input.SetValue("")
	key(a, st, "down")
	require.Equal(t, "next", enter(a.Actions(st)))
	key(a, st, "enter")
	require.Equal(t, "field:0", a.named.at, "enter goes to the empty name")
	require.Empty(t, a.named.err)
	key(a, st, "enter")
	require.Equal(t, "Give the drive a name.", a.named.err)
	require.Equal(t, name, a.step)
}

func TestDetailsBackButton(t *testing.T) {
	st := newState()
	a := details(t, st)
	key(a, st, "up", "up")
	require.Equal(t, "btn:back", a.named.at)
	key(a, st, "space")
	require.Equal(t, choose, a.step, "with sign in skipped, Back goes to Service")
}

func mounting(t *testing.T, st *core.State) *Add {
	t.Helper()
	a := details(t, st)
	require.NotNil(t, key(a, st, "enter"))
	require.Equal(t, connect, a.step)
	return a
}

func TestConnectWhileMounting(t *testing.T) {
	st := newState()
	a := mounting(t, st)
	require.False(t, a.Capturing())
	require.Equal(t, []core.Action{{Key: "esc", Label: "cancel"}}, a.Actions(st))
	win := a.window(st, 100, 40)
	require.Equal(t, "Cancel", win.Buttons[0].Label)
	require.Equal(t, 1, win.Active, "the busy primary lights, so enter can't cancel by accident")
	require.Nil(t, key(a, st, "enter"))
	require.Equal(t, connect, a.step)

	click(a, st, "btn:cancel")
	require.Equal(t, name, a.step, "Cancel stops and goes back to Details")
	require.Zero(t, a.run.req)
}

func TestConnectDone(t *testing.T) {
	st := newState()
	a := mounting(t, st)
	a.Update(core.Reply{Req: a.run.req, Val: api.Drive{Name: "Home NAS", State: api.Connected, Folder: "/home/me/dokky/Home NAS"}}, st)
	require.Equal(t, "btn:finder", a.run.at, "focus lands on the primary")
	require.Equal(t, 1, active(a, st))
	require.Equal(t, "open in "+core.FileManager(), enter(a.Actions(st)))
	requireAllButtons(t, a, st)
	key(a, st, "left")
	require.Equal(t, "btn:done", a.run.at)
	require.Equal(t, 0, active(a, st))
	require.Contains(t, sent(key(a, st, "enter")), tea.Msg(core.CloseWindow{Screen: a}))
}

func TestConnectFailed(t *testing.T) {
	st := newState()
	a := mounting(t, st)
	a.Update(core.Reply{Req: a.run.req, Err: errors.New("unreachable")}, st)
	require.Equal(t, "btn:retry", a.run.at)
	require.Equal(t, "try again", enter(a.Actions(st)))
	key(a, st, "down")
	require.Equal(t, "btn:back", a.run.at)
	key(a, st, "enter")
	require.Equal(t, name, a.step)
}

func editState() (*core.State, api.Drive) {
	st := newState()
	d := api.Drive{ID: "d1", Name: "Home NAS", ServiceType: webdav.Type, State: api.Connected, Folder: "/home/me/dokky/Home NAS", CacheMaxAge: "1h"}
	st.Snap.Drives = []api.Drive{d}
	return st, d
}

func TestEditButtons(t *testing.T) {
	st, d := editState()
	a := editing(st, d)
	require.False(t, a.Capturing(), "the list is not typed into, so ? works")
	require.Equal(t, -1, active(a, st), "no button lights while a row has focus")
	require.Equal(t, []string{"item:0", "item:1", "item:2", "item:3", "item:4", "item:5", "btn:remove", "btn:signin", "btn:view", "btn:mount"}, a.stops(st))
	requireAllButtons(t, a, st)

	key(a, st, "up")
	require.Equal(t, "btn:mount", a.ed.at)
	require.Equal(t, 3, active(a, st))
	require.Equal(t, "unmount", enter(a.Actions(st)))
	key(a, st, "left", "left", "left", "left")
	require.Equal(t, "btn:remove", a.ed.at, "left walks the bar and stops at its end")
	require.True(t, a.Actions(st)[0].Danger)
	require.NotNil(t, key(a, st, "enter"), "enter on Remove asks to remove")

	// the drive is already busy, so the mount itself sends nothing
	st.Busy = core.Flight{d.ID: true}
	a.ed.at = "btn:mount"
	require.Nil(t, key(a, st, "enter"), "enter on Unmount keeps the window open")
	require.Equal(t, "btn:mount", a.ed.at, "and keeps the button focused")

	st.Snap.Drives[0].State = api.Connecting
	require.Equal(t, "btn:mount", a.focused(st), "the mount button stays while it works")
	require.Empty(t, enter(a.Actions(st)), "and has nothing to press")
	require.Nil(t, key(a, st, "enter"))
}

func TestEditLoginSwitch(t *testing.T) {
	st, d := editState()
	a := editing(st, d)
	a.ed.at = rowID(eLogin)
	require.Contains(t, a.Actions(st), core.Action{Key: "← →", Label: "off/on"})
	require.Equal(t, "turn on", enter(a.Actions(st)))
	require.Nil(t, key(a, st, "left"), "left on an off switch does nothing")
	require.NotNil(t, key(a, st, "right"))
	require.True(t, a.loginOn(d))

	a.Update(core.Reply{Req: a.ed.req, Val: false}, st)
	st.Snap.Drives[0].AtLogin = true
	require.Nil(t, key(a, st, "right"), "right on an on switch does nothing")
	require.NotNil(t, key(a, st, "left"))
	require.False(t, a.loginOn(st.Snap.Drives[0]))
}

func TestEditClicks(t *testing.T) {
	st, d := editState()
	a := editing(st, d)
	click(a, st, rowID(eAge))
	require.True(t, a.ed.menu, "a click on Keep files for opens its list")
	click(a, st, rowID(eSize))
	require.False(t, a.ed.menu, "a click outside closes the list")
	require.Equal(t, rowID(eSize), a.ed.at, "then moves focus")
	require.Nil(t, a.ed.field, "a first click only focuses")
	click(a, st, rowID(eSize))
	require.NotNil(t, a.ed.field, "a click on the focused value edits it")
	require.True(t, a.Capturing())
	click(a, st, rowID(eSize))
	require.NotNil(t, a.ed.field, "a click inside keeps typing")
	click(a, st, rowID(eName))
	require.Nil(t, a.ed.field, "a click outside ends the edit")
	require.Equal(t, rowID(eName), a.ed.at)

	click(a, st, rowID(eAge))
	click(a, st, rowID(eAge))
	require.False(t, a.ed.menu, "a second click on Keep files for closes it")
	click(a, st, rowID(eAge))
	click(a, st, "btn:mount")
	require.False(t, a.ed.menu, "a button click closes the list too")
	require.Equal(t, "btn:mount", a.ed.at)
}

func TestEditEscBacksOutInLayers(t *testing.T) {
	st, d := editState()
	a := editing(st, d)
	a.ed.at = rowID(eAge)
	key(a, st, "enter")
	require.True(t, a.ed.menu)
	require.Nil(t, key(a, st, "esc"))
	require.False(t, a.ed.menu, "esc closes the list first")

	key(a, st, "up", "up", "up", "up", "up")
	require.Equal(t, rowID(eName), a.ed.at)
	require.Equal(t, "edit", enter(a.Actions(st)))
	key(a, st, "enter")
	require.NotNil(t, a.ed.field)
	require.Equal(t, "save", enter(a.Actions(st)))
	require.Nil(t, key(a, st, "esc"))
	require.Nil(t, a.ed.field, "then the value being typed")
	require.Contains(t, sent(key(a, st, "esc")), tea.Msg(core.CloseWindow{Screen: a}), "then the window")
}

func TestFootersNameOnlyKeysThatWork(t *testing.T) {
	st, d := editState()
	a := editing(st, d)
	for _, act := range a.Actions(st) {
		require.False(t, len(act.Key) == 1 && strings.Contains("mvsd", act.Key), "letters are shortcuts for the buttons, not footer hints")
	}
}
