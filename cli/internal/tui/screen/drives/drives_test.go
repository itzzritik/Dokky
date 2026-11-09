package drives

import (
	"strconv"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func state(wide bool) *core.State {
	return &core.State{W: 120, H: 30, Wide: wide, Now: time.Now(), Snap: api.State{Drives: []api.Drive{
		{ID: "1", Name: "Photos", State: api.Connected},
		{ID: "2", Name: "Work", State: api.Off},
		{ID: "3", Name: "Music", State: api.Connected},
	}}}
}

func press(s *Drives, st *core.State, keys ...string) {
	for _, k := range keys {
		s.Update(core.Press(k), st)
	}
}

func typed(s *Drives, st *core.State, text string) {
	for _, r := range text {
		press(s, st, string(r))
	}
}

func keys(acts []core.Action) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.Key] = a.Label
	}
	return out
}

func selected(s *Drives, st *core.State) string {
	d, _ := s.current(st)
	return d.Name
}

func TestEscClearsAFilter(t *testing.T) {
	st := state(true)
	s := New()
	press(s, st, "/")
	require.True(t, s.Capturing())
	typed(s, st, "zzz")
	press(s, st, "enter")
	require.False(t, s.Capturing())
	require.Empty(t, s.list(st))

	acts := keys(s.Actions(st))
	require.Equal(t, "find", acts["/"], "no match still says how to get out")
	require.Equal(t, "clear", acts["esc"])
	require.Equal(t, "add", acts["a"])

	press(s, st, "esc")
	require.Len(t, s.list(st), 3)
	require.NotContains(t, keys(s.Actions(st)), "esc")
}

func TestEscLeavesThePaneBeforeTheFilter(t *testing.T) {
	st := state(false)
	s := New()
	press(s, st, "/")
	typed(s, st, "o")
	press(s, st, "enter", "enter")
	require.True(t, s.open)
	press(s, st, "esc")
	require.False(t, s.open)
	require.Equal(t, "o", s.find.Value())
	press(s, st, "esc")
	require.Empty(t, s.find.Value())
}

func TestClickOnTheFindFieldStartsFinding(t *testing.T) {
	st := state(true)
	s := New()
	s.find.Input.SetValue("ph")
	s.Update(core.Click{ID: "find"}, st)
	require.True(t, s.Capturing())
}

func TestNarrowFooters(t *testing.T) {
	st := state(false)
	s := New()
	list := keys(s.Actions(st))
	require.Equal(t, "details", list["enter"])
	require.Equal(t, "unmount", list["m"], "the drive's keys show in the list too")
	require.Contains(t, list, "v")

	press(s, st, "enter")
	pane := keys(s.Actions(st))
	require.Equal(t, "next", pane["↑↓"])
	require.Equal(t, "back", pane["esc"])
	press(s, st, "down")
	require.Equal(t, "Work", selected(s, st))

	s.Update(core.SwitchTab{Tab: 2}, st)
	require.False(t, s.open, "leaving the tab closes it")
}

func TestHomeEndAndPages(t *testing.T) {
	st := state(true)
	for i := range 30 {
		st.Snap.Drives = append(st.Snap.Drives, api.Drive{ID: "x" + strconv.Itoa(i), Name: "Drive " + strconv.Itoa(i)})
	}
	s := New()
	s.View(st, 114, 13)
	press(s, st, "pgdown")
	require.Equal(t, 10, s.sel)
	press(s, st, "end")
	require.Equal(t, "Drive 29", selected(s, st))
	press(s, st, "home")
	require.Equal(t, "Photos", selected(s, st))
}
