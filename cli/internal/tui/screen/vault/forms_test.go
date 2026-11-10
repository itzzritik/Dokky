package vault

import (
	"os"
	"path/filepath"
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
	_, cmd := f.Update(core.Press(k), st)
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
		"no again": {goodPassword, "", 1, "Type it again to confirm."},
		"mismatch": {goodPassword, goodPassword + "x", 1, "The two passwords don't match."},
		"good":     {goodPassword, goodPassword, -1, ""},
	} {
		n := makeNew("Password", "Confirm")
		n.pw.Type(tea.PasteMsg{Content: c.pw})
		n.again.Type(tea.PasteMsg{Content: c.again})
		n.recheck()
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
	require.Equal(t, "field:1", c.at, "enter moves to the confirm field")
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
	require.Equal(t, "field:0", c.at)
	require.Equal(t, len(goodPassword), c.np.pw.Len(), "a failed call keeps what was typed")

	key(f, st, "enter")
	f.Update(core.Reply{Req: c.req, Val: "DK1-ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6789-AB"}, st)
	k, ok := f.top().(*showKey)
	require.True(t, ok, "the recovery key follows")
	require.Zero(t, c.np.pw.Len(), "the password is wiped once set")
	require.True(t, f.Holding())

	require.Equal(t, "btn:download", k.at, "focus starts on Download")
	key(f, st, "q")
	require.True(t, f.Holding(), "q never leaves a key on screen behind")

	key(f, st, "right")
	require.Equal(t, "btn:continue", k.at)
	key(f, st, "enter")
	require.Equal(t, "Not downloaded yet. Press Continue again to skip.", k.err)
	require.True(t, f.Holding(), "the first Continue without a download only warns")

	key(f, st, "left")
	require.Equal(t, "btn:download", k.at)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.NoError(t, os.Mkdir(filepath.Join(home, "Downloads"), 0o700))
	saved := sent(key(f, st, "enter"))
	require.Len(t, saved, 1)
	f.Update(saved[0], st)
	path := filepath.Join(home, "Downloads", "dokky-recovery-key.txt")
	require.Equal(t, path, k.saved)
	require.Equal(t, "btn:continue", k.at, "focus moves on to Continue once saved")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), "DK1-ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6789-AB")

	msgs := sent(key(f, st, "enter"))
	require.False(t, f.Holding())
	require.Contains(t, msgs, tea.Msg(Settled{}))
	require.Contains(t, msgs, tea.Msg(core.Toast{Text: encrypted}))
	require.Empty(t, k.key, "the key is wiped once done")
}

func TestKeyContinueTwiceSkipsTheDownload(t *testing.T) {
	st := newState()
	f := newFlow(st, newShowKey("DK1-ABCD", ""), true, exitStay)
	key(f, st, "down")
	key(f, st, "enter")
	require.True(t, f.Holding())
	sent(key(f, st, "space"))
	require.False(t, f.Holding(), "a key written down on paper is fine too")
}

func TestKeyButtonsByMouse(t *testing.T) {
	st := newState()
	f := newFlow(st, newShowKey("DK1-ABCD", ""), true, exitStay)
	k := f.top().(*showKey)
	f.Update(core.Click{ID: "btn:continue"}, st)
	require.Equal(t, "btn:continue", k.at, "a click moves focus to what it hit")
	require.NotEmpty(t, k.err)
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

func TestUnlockStartsSystemAuth(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultUnlocked
	f := Unlock(st, false)
	u := f.top().(*unlock)
	require.NotNil(t, f.Start(st), "the lock screen asks for Touch ID at once")
	require.True(t, u.system)
	require.Equal(t, []core.Action{{Key: "esc", Label: "Use password"}}, f.top().actions(st))

	key(f, st, "esc")
	require.Zero(t, u.req, "esc stops waiting")
	require.True(t, u.typing(), "and the password field takes over")

	key(f, st, "enter")
	require.True(t, u.system, "enter on the empty field asks for Touch ID again")
	key(f, st, "esc")

	typed(f, st, "secret")
	key(f, st, "enter")
	require.False(t, u.system, "a typed password is checked")
	f.Update(core.Reply{Req: u.req, Err: coded(api.ErrWrongPassword, "wrong")}, st)
	require.Equal(t, "That password is not right.", u.err)
	require.Zero(t, u.pw.Len(), "a wrong password is cleared")

	key(f, st, "enter")
	require.True(t, u.system, "enter on an empty field asks Touch ID")
	f.Update(core.Reply{Req: u.req, Err: coded(api.ErrCancelled, "cancelled")}, st)
	require.Empty(t, u.err, "a dismissed prompt is no error")
}

func TestUnlockOverSSHNeverAsksSystem(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	st.Deps.Remote = true
	f := Unlock(st, false)
	u := f.top().(*unlock)
	require.Nil(t, f.Start(st))
	key(f, st, "enter")
	require.Zero(t, u.req)
	require.Equal(t, "Type your master password.", u.err)
}

func TestUnlockSucceeds(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultUnlocked
	f := Unlock(st, true)
	u := f.top().(*unlock)
	require.True(t, u.force, "after a lock a live session is not enough")
	typed(f, st, "secret")
	key(f, st, "enter")
	_, cmd := f.Update(core.Reply{Req: u.req}, st)
	require.Contains(t, sent(cmd), tea.Msg(Released{}))
	require.Zero(t, u.pw.Len())
}

func TestForgotPassword(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultLocked
	f := Unlock(st, false)
	key(f, st, "down")
	require.False(t, f.top().typing(), "the link has focus")
	key(f, st, "enter")
	r, ok := f.top().(*recoverForm)
	require.True(t, ok)
	require.False(t, r.join)
	key(f, st, "esc")
	_, ok = f.top().(*unlock)
	require.True(t, ok, "esc goes back")
}

func TestJoinUsesRecoveryKeyForTheAccount(t *testing.T) {
	st := newState()
	st.Snap.Account = api.Account{LoggedIn: true, Pending: true, Email: "you@example.com"}
	f := SignIn(st, api.NeedJoin, func(*core.State) tea.Cmd { return nil })
	require.True(t, f.SignInStep())
	j := f.top().(*join)
	key(f, st, "enter")
	require.Equal(t, "Type your master password.", j.err)
	key(f, st, "down")
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
	st.Deps.Remote = true
	f := Unlock(st, false)
	typed(f, st, "q")
	require.Equal(t, 1, f.top().(*unlock).pw.Len(), "q is a letter in the password field")
	key(f, st, "down")
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
	require.Equal(t, "field:1", c.at)
}

func TestFormsWalkToEveryButton(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultUnlocked
	for name, c := range map[string]struct {
		f     *Flow
		stops []string
	}{
		"change":   {newFlow(st, newChange(), true, exitClose), changeStops},
		"new key":  {newFlow(st, newAskKey(), true, exitClose), askKeyStops},
		"create":   {Setup(st), createStops},
		"recover":  {newFlow(st, newRecover(false), true, exitClose), recoverStops},
		"join":     {newFlow(st, newJoin(""), true, exitClose), joinStops},
		"show key": {newFlow(st, newShowKey("DK1-ABCD", ""), true, exitStay), keyStops},
	} {
		var seen []string
		for range c.stops {
			seen = append(seen, focusOf(c.f.top()))
			key(c.f, st, "down")
		}
		require.ElementsMatch(t, c.stops, seen, name+": tab reaches every stop")
		require.Equal(t, c.stops[0], focusOf(c.f.top()), name+": and wraps")
		key(c.f, st, "up")
		require.Equal(t, c.stops[len(c.stops)-1], focusOf(c.f.top()), name+": shift+tab goes back")
	}
}

func TestCancelButtonsByKeyboard(t *testing.T) {
	st := newState()
	st.Snap.Vault.State = api.VaultUnlocked
	for name, fm := range map[string]form{"change": newChange(), "new key": newAskKey()} {
		f := newFlow(st, fm, true, exitClose)
		for focusOf(f.top()) != "btn:cancel" {
			key(f, st, "down")
		}
		require.Equal(t, []tea.Msg{core.CloseWindow{Screen: f}}, sent(key(f, st, "enter")), name)
	}
}

func focusOf(fm form) string {
	switch v := fm.(type) {
	case *create:
		return v.at
	case *change:
		return v.at
	case *askKey:
		return v.at
	case *recoverForm:
		return v.at
	case *join:
		return v.at
	case *showKey:
		return v.at
	case *unlock:
		return v.at
	}
	return ""
}
