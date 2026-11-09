package settings

import (
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

func signedIn() *core.State {
	return &core.State{W: 100, H: 30, Wide: true, Now: time.Now(), Snap: api.State{
		Version: "1.0.0",
		Account: api.Account{LoggedIn: true, Email: "you@example.com"},
		Vault:   api.Vault{State: api.VaultUnlocked},
	}}
}

func press(s *Settings, st *core.State, keys ...string) {
	for _, k := range keys {
		s.Update(core.Press(k), st)
	}
}

func label(s *Settings, st *core.State) string { return s.selected(s.rows(st)).label }

func has(acts []core.Action, key, label string) bool {
	for _, a := range acts {
		if a.Key == key && a.Label == label {
			return true
		}
	}
	return false
}

func TestSelectionFollowsTheRowAsRowsComeAndGo(t *testing.T) {
	st := signedIn()
	st.Latest = "1.1.0"
	s := New()
	press(s, st, "down", "down")
	require.Equal(t, "Cached files", label(s, st))

	st.Latest = "" // the update row above it goes
	require.Equal(t, "Cached files", label(s, st))
	st.Latest = "1.2.0"
	require.Equal(t, "Cached files", label(s, st))

	s.Update(core.Click{ID: "row:Your devices"}, st)
	st.Snap.Vault.Need = api.NeedJoin // Sync is paused shows up above it
	require.Equal(t, "Your devices", label(s, st))
	st.Snap.Vault.Need = ""
	require.Equal(t, "Your devices", label(s, st))
	press(s, st, "down")
	require.Equal(t, "Remove this device", label(s, st))
}

func TestSpaceFlipsSwitchesOnly(t *testing.T) {
	st := signedIn()
	s := New()
	press(s, st, "end")
	require.Equal(t, "Uninstall dokky", label(s, st))
	_, cmd := s.Update(core.Press("space"), st)
	require.Nil(t, cmd, "space never starts an uninstall")
	_, cmd = s.Update(core.Press("enter"), st)
	require.NotNil(t, cmd, "enter does")

	press(s, st, "home")
	require.Equal(t, "Notify when a drive is ejected", label(s, st))
	require.True(t, has(s.Actions(st), "space", "turn on"))
	_, cmd = s.Update(core.Press("space"), st)
	require.NotNil(t, cmd)
}

func TestHomeEndAndPages(t *testing.T) {
	st := signedIn()
	s := New()
	s.View(st, 94, 10) // a page of 4 rows
	press(s, st, "pgdown")
	require.Equal(t, 4, s.sel)
	press(s, st, "pgup", "pgup")
	require.Equal(t, 0, s.sel)
	press(s, st, "end")
	require.Equal(t, len(s.rows(st))-1, s.sel)
	press(s, st, "home")
	require.Equal(t, 0, s.sel)
}

func TestClickMovesTheSelection(t *testing.T) {
	st := signedIn()
	s := New()
	_, cmd := s.Update(core.Click{ID: "row:Your devices"}, st)
	require.Equal(t, "Your devices", label(s, st))
	require.NotNil(t, cmd)
}

func TestSignedOutSaysWhatSDoes(t *testing.T) {
	st := signedIn()
	st.Snap.Account.SignedOut = true
	s := New()
	require.True(t, has(s.Actions(st), "s", "remove this device"))
	_, hits, _ := ui.Compose(94, 30, ui.Pass{Layer: s.View(st, 94, 30)})
	found := false
	for y := range 3 {
		for x := range 94 {
			found = found || hits.At(x, y) == "key:s"
		}
	}
	require.True(t, found, "the banner's key can be clicked")
}
