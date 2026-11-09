package vault

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

const goodPassword = "Correct-Horse-9"

func newState() *core.State {
	return &core.State{Now: time.Now(), Connected: true, W: 100, H: 30,
		Snap: api.State{Vault: api.Vault{State: api.VaultSetup, System: "Touch ID", PasswordDays: 14}}}
}

func key(f *Flow, st *core.State, k string) tea.Cmd {
	msg := core.Press(k)
	switch k {
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "ctrl+r":
		msg = tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	}
	_, cmd := f.Update(msg, st)
	return cmd
}

func typed(f *Flow, st *core.State, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)}, st)
	}
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

func TestNewPasswordRules(t *testing.T) {
	for name, c := range map[string]struct {
		pw, again string
		at        int
		msg       string
	}{
		"empty":    {"", "", 0, "Type a master password."},
		"short":    {"abc", "", 0, "Use at least 8 characters."},
		"common":   {"password123", "password123", 0, "This is a common password, pick something less guessable."},
		"no again": {goodPassword, "", 1, "Type it again to confirm."},
		"mismatch": {goodPassword, goodPassword + "x", 1, "The two passwords don't match."},
		"good":     {goodPassword, goodPassword, -1, ""},
	} {
		n := makeNew("Password")
		n.pw.Type(tea.PasteMsg{Content: c.pw})
		n.again.Type(tea.PasteMsg{Content: c.again})
		n.rescore()
		at, msg := n.problem()
		require.Equal(t, c.at, at, name)
		require.Equal(t, c.msg, msg, name)
	}
}

func TestCreateThenKey(t *testing.T) {
	st := newState()
	f := Setup(st)
	c := f.top().(*create)

	typed(f, st, goodPassword)
	key(f, st, "enter")
	require.Equal(t, 1, c.focus, "enter moves to the confirm field")
	typed(f, st, "Correct-Horse-8")
	key(f, st, "enter")
	require.Equal(t, "The two passwords don't match.", c.err)
	require.Zero(t, c.req, "nothing is sent while the form is wrong")

	for range len("8") {
		key(f, st, "backspace")
	}
	typed(f, st, "9")
	require.NotNil(t, key(f, st, "enter"))
	require.NotZero(t, c.req)
	require.True(t, f.Holding(), "the app keeps the screen while setup runs")

	f.Update(core.Reply{Req: c.req, Err: coded(api.ErrWeakPassword, "pick something less guessable")}, st)
	require.Equal(t, "Pick something less guessable.", c.err)
	require.Equal(t, 0, c.focus)
	require.Equal(t, len(goodPassword), c.np.pw.Len(), "a failed call keeps what was typed")

	key(f, st, "enter")
	f.Update(core.Reply{Req: c.req, Val: "DK1-ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6789-AB"}, st)
	k, ok := f.top().(*showKey)
	require.True(t, ok, "the recovery key follows")
	require.Zero(t, c.np.pw.Len(), "the password is wiped once set")
	require.True(t, f.Holding())

	key(f, st, "q")
	key(f, st, "enter")
	require.Equal(t, "Press space once your key is saved.", k.err)
	require.True(t, f.Holding(), "neither q nor enter leaves an unsaved key")

	key(f, st, "space")
	msgs := sent(key(f, st, "enter"))
	require.False(t, f.Holding())
	require.Contains(t, msgs, tea.Msg(Settled{}))
	require.Contains(t, msgs, tea.Msg(core.Toast{Text: encrypted}))
	require.Empty(t, k.key, "the key is wiped once saved")
}

func TestRevealShowsBothFields(t *testing.T) {
	st := newState()
	f := Setup(st)
	c := f.top().(*create)
	key(f, st, "ctrl+r")
	require.True(t, c.np.pw.Reveal)
	require.True(t, c.np.again.Reveal)
	key(f, st, "ctrl+r")
	require.False(t, c.np.pw.Reveal)
}

func TestUnlockEnter(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	f := Unlock(st)
	u := f.top().(*unlock)

	key(f, st, "enter")
	require.True(t, u.system, "enter on an empty field asks Touch ID")
	require.NotZero(t, u.req)
	key(f, st, "esc")
	require.Zero(t, u.req, "esc stops waiting")
	require.False(t, u.system)

	typed(f, st, "secret")
	key(f, st, "enter")
	require.False(t, u.system, "a typed password is checked")
	f.Update(core.Reply{Req: u.req, Err: coded(api.ErrWrongPassword, "wrong")}, st)
	require.Equal(t, "That password is not right.", u.err)
	require.Zero(t, u.pw.Len(), "a wrong password is cleared")

	st.Snap.Vault.CooldownUntil = st.Now.Add(time.Minute)
	typed(f, st, "secret")
	key(f, st, "enter")
	require.Zero(t, u.req, "nothing is tried during a cooldown")
}

func TestUnlockOverSSHNeverAsksSystem(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	st.Deps.Remote = true
	f := Unlock(st)
	u := f.top().(*unlock)
	key(f, st, "enter")
	require.Zero(t, u.req)
	require.Equal(t, "Type your master password.", u.err)
}

func TestUnlockSucceeds(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultUnlocked
	f := WindowLock(st)
	u := f.top().(*unlock)
	typed(f, st, "secret")
	key(f, st, "enter")
	_, cmd := f.Update(core.Reply{Req: u.req, Val: "grant"}, st)
	require.Contains(t, sent(cmd), tea.Msg(Released{}))
	require.Zero(t, u.pw.Len())
}

func TestForgotPassword(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	f := Unlock(st)
	key(f, st, "tab")
	require.False(t, f.top().typing(), "the link has focus")
	key(f, st, "enter")
	r, ok := f.top().(*recoverForm)
	require.True(t, ok)
	require.False(t, r.join)
	key(f, st, "esc")
	_, ok = f.top().(*unlock)
	require.True(t, ok, "esc goes back")

	w := WindowLock(st)
	key(w, st, "tab")
	require.Equal(t, 0, w.top().(*unlock).focus, "the window lock has no forgot link")
}

func TestJoinUsesRecoveryKeyForTheAccount(t *testing.T) {
	st := newState()
	st.Snap.Account = api.Account{LoggedIn: true, Pending: true, Email: "you@example.com"}
	f := SignIn(st, api.NeedJoin, func(*core.State) tea.Cmd { return nil })
	require.True(t, f.SignInStep())
	j := f.top().(*join)
	key(f, st, "enter")
	require.Equal(t, "Type your master password.", j.err)
	key(f, st, "tab")
	key(f, st, "enter")
	r, ok := f.top().(*recoverForm)
	require.True(t, ok)
	require.True(t, r.join, "the account's vault is joined with its recovery key")
	key(f, st, "esc")
	require.Nil(t, key(f, st, "esc"), "sign in can't skip its master password step")
}

func TestSignInCreatesWhenTheAccountHasNoVault(t *testing.T) {
	st := newState()
	f := SignIn(st, api.NeedCreate, nil)
	_, ok := f.top().(*create)
	require.True(t, ok)
}

func TestFlowExits(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	f := Unlock(st)
	typed(f, st, "q")
	require.Equal(t, 1, f.top().(*unlock).pw.Len(), "q is a letter in the password field")
	key(f, st, "tab")
	require.Equal(t, []tea.Msg{core.Quit{}}, sent(key(f, st, "q")), "q quits off the field")

	w := newFlow(st, newChange(), true, exitClose)
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: w}}, sent(key(w, st, "esc")))

	typed(w, st, "secret")
	_, cmd := w.Update(core.Locked{}, st)
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: w}}, sent(cmd), "the window lock closes it")
	require.Zero(t, w.top().(*change).cur.Len(), "and wipes what was typed")

	s := SignIn(st, api.NeedJoin, nil)
	typed(s, st, "secret")
	_, cmd = s.Update(core.Locked{}, st)
	require.Nil(t, cmd, "the sign in step stays")
	require.Zero(t, s.top().(*join).pw.Len())
}

func TestChangeRules(t *testing.T) {
	st := newState()
	f := newFlow(st, newChange(), true, exitClose)
	c := f.top().(*change)
	key(f, st, "enter")
	require.Equal(t, "Type your current master password.", c.err)
	typed(f, st, goodPassword)
	key(f, st, "enter")
	typed(f, st, goodPassword)
	key(f, st, "enter")
	typed(f, st, goodPassword)
	key(f, st, "enter")
	require.Equal(t, "That is the password you have now.", c.err)
	require.Equal(t, 1, c.focus)
}

func TestPick(t *testing.T) {
	st := newState()
	st.Snap.Vault = api.Vault{State: api.VaultUnlocked, Unlock: api.UnlockAuto}
	cmd := PickUnlock(st)
	f := sent(cmd)[0].(core.OpenWindow).Screen.(*Flow)
	pk := f.top().(*pick)
	require.Equal(t, "Not available on this computer", pk.opts[1].off)

	key(f, st, "down")
	require.Nil(t, key(f, st, "enter"))
	require.Equal(t, "Not available on this computer", pk.err)

	key(f, st, "up")
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: f}}, sent(key(f, st, "enter")), "the current choice changes nothing")
}

func TestMeterWords(t *testing.T) {
	for pw, want := range map[string]string{
		"":                 "",
		"abc12":            "Too short",
		"password123":      "Too common",
		" Kx9#mP2$vL":      "Trim spaces",
		"abcdefghijkl":     "Weak",
		"Kx9#mP2$":         "Okay",
		"Kx9#mP2$vL":       "Good",
		"G7#kq!Vz2@pLw9$e": "Strong",
	} {
		n := makeNew("Password")
		for _, r := range pw {
			n.pw.Type(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		n.rescore()
		got, _ := n.status()
		require.Equal(t, want, got, pw)
	}
}
