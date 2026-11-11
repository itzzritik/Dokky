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

func TestAddADriveIsAKeyNotAnItem(t *testing.T) {
	st := state(api.Drive{ID: "d1", Name: "Photos"})
	o := New()
	require.Equal(t, []string{"row:d1"}, ids(o.items(st)), "only the drives; adding lives on Drives and the a key")
	acts := keys(o.Actions(st))
	require.Equal(t, "show", acts["enter"])
	require.Equal(t, "add drive", acts["a"])
	press(o, st, "down")
	require.Equal(t, "row:d1", selected(o, st))
	_, cmd := o.Update(core.Press("a"), st)
	require.NotNil(t, cmd, "a opens Add a drive")
	out, _, _ := ui.Compose(94, 26, ui.Pass{Layer: o.View(st, 94, 26)})
	require.NotContains(t, out, "Add a drive")
}

func ids(items []item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.id)
	}
	return out
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
