package overview

import (
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

func state(drives ...api.Drive) *core.State {
	return &core.State{W: 100, H: 30, Wide: true, Now: time.Now(), Snap: api.State{Drives: drives, Account: api.Account{LoggedIn: true}},
		Catalog: []api.Provider{{Type: "drive", Name: "Google Drive", Popular: true}, {Type: "sftp", Name: "SFTP", Group: "own", Popular: true}}}
}

func press(o *Overview, st *core.State, keys ...string) {
	for _, k := range keys {
		o.Update(core.Press(k), st)
	}
}

func keys(acts []core.Action) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.Key] = a.Label
	}
	return out
}

func selected(o *Overview, st *core.State) string {
	it, _ := o.current(o.items(st))
	return it.id
}

func TestAddADriveIsAnItem(t *testing.T) {
	st := state(api.Drive{ID: "d1", Name: "Photos"})
	o := New()
	acts := keys(o.Actions(st))
	require.Equal(t, "move", acts["↑↓"], "one drive and the add item")
	require.Equal(t, "show", acts["enter"])

	press(o, st, "down")
	require.Equal(t, "row:add", selected(o, st))
	acts = keys(o.Actions(st))
	require.Equal(t, "add drive", acts["enter"])
	require.NotContains(t, acts, "a", "enter already says it")
	_, cmd := o.Update(core.Press("enter"), st)
	require.NotNil(t, cmd)

	press(o, st, "up")
	_, cmd = o.Update(core.Click{ID: "row:add"}, st)
	require.Equal(t, "row:add", selected(o, st), "a click moves the selection")
	require.NotNil(t, cmd)

	// drawn under the cards, where a click finds it
	_, hits, _ := ui.Compose(94, 26, ui.Pass{Layer: o.View(st, 94, 26)})
	found := false
	for y := range 26 {
		found = found || hits.At(10, y) == "row:add"
	}
	require.True(t, found)
}

func TestSelectionFollowsTheDrive(t *testing.T) {
	st := state(api.Drive{ID: "d1", Name: "Photos"}, api.Drive{ID: "d2", Name: "Work"})
	o := New()
	press(o, st, "down")
	require.Equal(t, "row:d2", selected(o, st))
	st.Snap.Drives = append([]api.Drive{{ID: "d0", Name: "Music"}}, st.Snap.Drives...)
	require.Equal(t, "row:d2", selected(o, st))
	_, cmd := o.Update(core.Press("enter"), st)
	require.Equal(t, core.SwitchTab{Tab: core.DrivesTab, Drive: "d2"}, cmd())
}

func TestSignInIsAnItemWhenEmpty(t *testing.T) {
	st := state()
	st.Snap.Account.LoggedIn = false
	o := New()
	press(o, st, "end")
	require.Equal(t, "row:signin", selected(o, st))
	acts := keys(o.Actions(st))
	require.Equal(t, "sign in", acts["enter"])
	require.NotContains(t, acts, "s")
	_, cmd := o.Update(core.Press("enter"), st)
	require.NotNil(t, cmd)

	press(o, st, "home")
	require.Equal(t, "row:drive", selected(o, st))
	require.Equal(t, "sign in", keys(o.Actions(st))["s"], "s stays as a shortcut")
	_, cmd = o.Update(core.Click{ID: "row:signin"}, st)
	require.NotNil(t, cmd)

	st.Snap.Account.LoggedIn = true
	require.NotContains(t, o.items(st), item{id: "row:signin", signIn: true})
}

func TestPagesInTheWelcome(t *testing.T) {
	st := state()
	for i := range 30 {
		st.Catalog = append(st.Catalog, api.Provider{Type: "p" + string(rune('A'+i)), Name: "P", Popular: true})
	}
	o := New()
	o.View(st, 94, 20)
	press(o, st, "pgdown")
	require.Equal(t, o.page, o.sel)
	press(o, st, "end")
	require.Equal(t, len(o.items(st))-1, o.sel)
	press(o, st, "home")
	require.Equal(t, 0, o.sel)
}
