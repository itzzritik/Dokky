package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

func newApp(s api.State) *App {
	a := New(core.Deps{})
	a.veil, a.updateChecked = false, true
	a.st.W, a.st.H, a.st.Connected = 100, 30, true
	a.setSnap(s)
	return a
}

func vaultIn(state string) api.Vault {
	// an unlocked dokky that is open has a control session
	return api.Vault{State: state, System: "Touch ID", PasswordDays: 15, Session: state == api.VaultUnlocked}
}

func TestCoverFor(t *testing.T) {
	signedIn := api.Account{LoggedIn: true, Email: "you@example.com"}
	pending := api.Account{LoggedIn: true, Pending: true}
	chosen := api.Settings{Chosen: true}
	for name, c := range map[string]struct {
		snap      api.State
		connected bool
		want      coverKind
	}{
		"not connected":              {api.State{Vault: vaultIn(api.VaultLocked)}, false, noCover},
		"locked":                     {api.State{Vault: vaultIn(api.VaultLocked), Settings: chosen}, true, unlockCover},
		"locked before the gate":     {api.State{Vault: vaultIn(api.VaultLocked)}, true, unlockCover},
		"fresh install":              {api.State{Vault: vaultIn(api.VaultSetup)}, true, gateCover},
		"chose this computer":        {api.State{Vault: vaultIn(api.VaultSetup), Settings: chosen}, true, createCover},
		"older install with drives":  {api.State{Vault: vaultIn(api.VaultSetup), Drives: []api.Drive{{ID: "d"}}}, true, createCover},
		"older signed-in install":    {api.State{Vault: vaultIn(api.VaultSetup), Account: signedIn}, true, createCover},
		"account has a vault":        {api.State{Vault: api.Vault{State: api.VaultSetup, Need: api.NeedJoin}, Account: signedIn}, true, joinCover},
		"sign in shows its own step": {api.State{Vault: api.Vault{State: api.VaultSetup, Need: api.NeedCreate}, Account: pending}, true, noCover},
		"unlocked":                   {api.State{Vault: vaultIn(api.VaultUnlocked), Settings: chosen}, true, noCover},
		"no control session":         {api.State{Vault: api.Vault{State: api.VaultUnlocked}, Settings: chosen}, true, unlockCover},
		"daemon without a vault":     {api.State{Settings: chosen}, true, noCover},
		"old daemon, fresh install":  {api.State{}, true, gateCover},
		"device removed":             {api.State{Vault: vaultIn(api.VaultLocked), Removed: &api.Removed{}}, true, noCover},
	} {
		st := &core.State{Connected: c.connected, Snap: c.snap}
		require.Equal(t, c.want, coverFor(st), name)
	}
}

func TestSignInStepOpensOnce(t *testing.T) {
	s := api.State{Vault: api.Vault{State: api.VaultSetup, Need: api.NeedJoin}, Account: api.Account{LoggedIn: true, Pending: true}}
	a := newApp(s)
	require.Nil(t, a.gate, "no full screen while sign in shows its step")
	require.Len(t, a.windows, 1)
	f, ok := a.windows[0].(*vault.Flow)
	require.True(t, ok && f.SignInStep())

	a.setSnap(s)
	require.Len(t, a.windows, 1, "never twice")
	a.update(core.OpenWindow{Screen: vault.SignIn(a.st, api.NeedJoin, nil)})
	require.Len(t, a.windows, 1, "a handover racing the app is dropped")

	b := newApp(api.State{Vault: vaultIn(api.VaultSetup)})
	b.windows = []core.Screen{&gate.SignIn{}}
	b.setSnap(s)
	require.Len(t, b.windows, 1, "the device code window hands over itself")

	a.update(core.CloseWindow{Screen: f})
	a.setSnap(s)
	require.Empty(t, a.windows, "a state from before the step finished doesn't open it again")
}

type held struct{ hold bool }

func (h *held) Update(tea.Msg, *core.State) (core.Screen, tea.Cmd) { return h, nil }
func (h *held) View(*core.State, int, int) *lipgloss.Layer         { return lipgloss.NewLayer("") }
func (h *held) Actions(*core.State) []core.Action                  { return nil }
func (h *held) Animating(*core.State) bool                         { return false }
func (h *held) Holding() bool                                      { return h.hold }

func TestRouteKeepsAScreenMidStep(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultSetup), Settings: api.Settings{Chosen: true}})
	_, ok := a.gate.(*vault.Flow)
	require.True(t, ok, "setup covers the tabs")

	h := &held{hold: true}
	a.gate = h
	a.setSnap(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	require.Same(t, h, a.gate, "the recovery key stays up after the vault unlocks")

	h.hold = false
	a.update(vault.Settled{})
	require.Nil(t, a.gate)
}

func TestDaemonLockClosesWindows(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	a.windows = []core.Screen{&held{}}
	a.setSnap(api.State{Vault: vaultIn(api.VaultLocked), Settings: api.Settings{Chosen: true}})
	require.Empty(t, a.windows)
	require.Equal(t, unlockCover, a.covered)
	require.IsType(t, &vault.Flow{}, a.gate)

	a.setSnap(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	require.Nil(t, a.gate, "unlocked, the tabs are back")
}

func TestLockKey(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	a.windows = []core.Screen{&held{}}
	a.press(core.Press("l"))
	require.True(t, a.st.Locked)
	require.Empty(t, a.windows, "locking closes every window, as Forged does")
	require.Equal(t, unlockCover, a.covered)
	a.press(core.Press("2"))
	require.Equal(t, 0, a.active, "keys go to the lock screen")

	a.update(vault.Released{})
	require.False(t, a.st.Locked)
	require.Nil(t, a.gate)
	a.press(core.Press("2"))
	require.Equal(t, 1, a.active)

	a.press(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	require.False(t, a.st.Locked, "keys are single keys: ctrl+l does nothing")

	b := newApp(api.State{Vault: vaultIn(api.VaultSetup), Settings: api.Settings{Chosen: true}})
	b.press(core.Press("l"))
	require.False(t, b.st.Locked, "nothing to lock without a master password")
}

func TestSessionLossLocks(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	a.windows = []core.Screen{&held{}}
	ended := vaultIn(api.VaultUnlocked)
	ended.Session = false
	a.setSnap(api.State{Vault: ended, Settings: api.Settings{Chosen: true}})
	require.True(t, a.st.Locked, "the screen locked or the computer slept")
	require.Empty(t, a.windows)
	require.Equal(t, unlockCover, a.covered)
}

func TestIdleLock(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	require.Equal(t, 4*time.Minute, a.idleArmed, "a fixed 4 minutes, as Forged")
	gen := a.idleGen

	a.st.Now = a.lastInput.Add(3 * time.Minute)
	require.NotNil(t, a.onIdle(idleCheck{gen}), "touched recently, it waits again")
	require.False(t, a.st.Locked)

	a.st.Now = a.lastInput.Add(5 * time.Minute)
	a.onIdle(idleCheck{gen})
	require.False(t, a.st.Locked, "a stale timer does nothing")
	a.onIdle(idleCheck{a.idleGen})
	require.True(t, a.st.Locked)
}

func TestLockedHidesToasts(t *testing.T) {
	a := newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: api.Settings{Chosen: true}})
	a.lockWindow(true)
	a.post(core.Toast{Text: "Home NAS mounted"})
	require.Nil(t, a.toast, "notices wait behind the lock")
	a.update(vault.Released{})
	require.NotNil(t, a.toast)
}

func typeKeys(a *App, s string) {
	for _, r := range s {
		a.press(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// runs a screen's commands that only send messages and hands those to the app; what the app returns, like its timers, is not run
func deliver(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			deliver(a, c)
		}
	case nil:
	default:
		a.update(m)
	}
}

func TestSetupKeepsTheKeyUpThenSaysSo(t *testing.T) {
	chosen := api.Settings{Chosen: true}
	a := newApp(api.State{Vault: vaultIn(api.VaultSetup), Settings: chosen})
	typeKeys(a, "Correct-Horse-9")
	a.press(core.Press("enter"))
	typeKeys(a, "Correct-Horse-9")
	require.NotNil(t, a.press(core.Press("enter")))
	req := core.NextReq() - 1

	a.setSnap(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: chosen})
	require.NotNil(t, a.gate, "the daemon unlocked before its reply came")
	a.update(core.Reply{Req: req, Epoch: a.st.Epoch, Val: "DK1-ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6789-AB"})
	a.setSnap(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: chosen})
	require.NotNil(t, a.gate, "the recovery key stays until it is saved")

	a.press(core.Press("q"))
	require.NotNil(t, a.gate, "q doesn't leave an unsaved key")
	a.press(core.Press("right"))
	a.press(core.Press("enter"))
	require.NotNil(t, a.gate, "Continue without a download asks once more")
	deliver(a, a.press(core.Press("enter")))
	require.Nil(t, a.gate)
	require.NotNil(t, a.toast)
	require.Equal(t, "Master password set. dokky is end-to-end encrypted.", a.toast.Text)
}

func TestUpdatingThenUnlockOnALockedDaemon(t *testing.T) {
	a := shown(100, 30)
	a.st.Connected, a.updateChecked = true, true
	a.carry = &carry{from: "0.1.4", to: "0.1.5"}
	a.post(core.Toast{Text: "Fixed 1 problem"})
	chosen := api.Settings{Chosen: true}

	require.NotNil(t, a.setSnap(api.State{Version: "0.1.5", Vault: vaultIn(api.VaultLocked), Settings: chosen}))
	require.True(t, a.updatingNow(), "Updating finishes on top")
	require.IsType(t, &vault.Flow{}, a.gate, "the unlock screen waits under it")
	text := strings.Join(screen(a), "\n")
	require.Contains(t, text, "Updated")
	require.NotContains(t, text, "Unlock dokky")
	require.Nil(t, a.press(core.Press("x")), "keys don't reach the screen under Updating")

	a.onSettled()
	require.False(t, a.updatingNow())
	require.Contains(t, strings.Join(screen(a), "\n"), "Unlock dokky")
	require.Nil(t, a.toast, "notices wait behind the unlock screen")
	require.Equal(t, []core.Toast{{Text: "Updated to v0.1.5"}, {Text: "Fixed 1 problem"}}, a.pending)

	a.setSnap(api.State{Version: "0.1.5", Vault: vaultIn(api.VaultUnlocked), Settings: chosen})
	require.Nil(t, a.gate)
	require.Equal(t, "Updated to v0.1.5", a.toast.Text, "in the order they came")
	require.Equal(t, []core.Toast{{Text: "Fixed 1 problem"}}, a.pending)
}

// the lock screen waits on Touch ID, so its card offers the password instead
func asking(a *App) bool {
	out, _, _ := ui.Compose(100, 30, ui.Pass{Layer: a.gate.(*vault.Flow).View(a.st, 100, 30)})
	return strings.Contains(out, "Use password")
}

// opening dokky on a lock set with l: the reconnect notice goes out first, so it can't cancel the new request
func TestOpeningOnALockAsksForTouchID(t *testing.T) {
	a := New(core.Deps{})
	a.updateChecked = true
	a.st.W, a.st.H = 100, 30
	ended := vaultIn(api.VaultUnlocked)
	ended.Session = false
	a.onConnected(connected{snap: api.State{Vault: ended, Settings: api.Settings{Chosen: true}}})
	require.True(t, asking(a), "Touch ID shows by itself as dokky opens")
}

func TestLockScreenAsksByItselfUnlessLockedByHand(t *testing.T) {
	chosen := api.Settings{Chosen: true}
	v := vaultIn(api.VaultLocked)
	v.System = ""
	a := newApp(api.State{Vault: v, Settings: chosen})
	require.False(t, asking(a), "dokky opened before the daemon's system check answered")
	v.System = "Touch ID"
	a.setSnap(api.State{Vault: v, Settings: chosen})
	require.True(t, asking(a), "the check answered: Touch ID shows by itself")

	ended := vaultIn(api.VaultUnlocked)
	ended.Session = false
	a = newApp(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: chosen})
	a.lockWindow(true)
	require.False(t, asking(a), "locked with l, it waits for the person")
	a.setSnap(api.State{Vault: ended, Settings: chosen})
	require.False(t, asking(a), "and keeps waiting as the session ends")
	a.update(vault.Released{})
	a.setSnap(api.State{Vault: vaultIn(api.VaultUnlocked), Settings: chosen})

	a.lockWindow(false)
	require.True(t, asking(a), "the idle lock asks, as before")
}
