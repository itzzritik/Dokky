package transfers

import (
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func moving(name string) api.Transfer {
	return api.Transfer{DriveID: "d1", Name: name, Up: true, Size: 100, Done: 40}
}

func failed(name string) api.Transfer {
	return api.Transfer{DriveID: "d1", Name: name, Up: true, Size: 100, Error: "no space left", At: time.Now()}
}

func state(wide bool) *core.State {
	return &core.State{W: 120, H: 30, Wide: wide, Now: time.Now(),
		Snap: api.State{Drives: []api.Drive{{ID: "d1", Name: "Photos", State: api.Connected}}},
		Act:  api.Activity{Now: []api.Transfer{moving("a.jpg"), moving("b.jpg")}, Earlier: []api.Transfer{failed("c.jpg")}}}
}

func press(s *Transfers, st *core.State, keys ...string) {
	for _, k := range keys {
		s.Update(core.Press(k), st)
	}
}

func name(s *Transfers, st *core.State) string {
	e, _ := s.current(st)
	return e.t.Name
}

func keys(acts []core.Action) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.Key] = a.Label
	}
	return out
}

func TestSelectionFollowsTheFile(t *testing.T) {
	st := state(true)
	s := New()
	press(s, st, "end")
	require.Equal(t, "c.jpg", name(s, st))
	require.Equal(t, "try now", keys(s.Actions(st))["enter"])

	// new files above it don't move the selection, so enter still retries c.jpg
	st.Act.Now = append([]api.Transfer{moving("new.jpg")}, st.Act.Now...)
	require.Equal(t, "c.jpg", name(s, st))

	press(s, st, "home", "down", "down")
	require.Equal(t, "b.jpg", name(s, st))
	// b finishes and moves to Earlier
	st.Act.Now = st.Act.Now[:2]
	st.Act.Earlier = append([]api.Transfer{{DriveID: "d1", Name: "b.jpg", Up: true, Size: 100, Done: 100, At: time.Now()}}, st.Act.Earlier...)
	e, _ := s.current(st)
	require.Equal(t, "b.jpg", e.t.Name)
	require.True(t, e.earlier)
}

func TestHomeEndAndPages(t *testing.T) {
	st := state(true)
	for i := range 30 {
		st.Act.Earlier = append(st.Act.Earlier, failed("f"+string(rune('a'+i))+".jpg"))
	}
	s := New()
	s.View(st, 114, 26)
	press(s, st, "pgdown")
	require.Equal(t, s.page, s.sel)
	press(s, st, "end")
	require.Equal(t, len(all(st))-1, s.sel)
	press(s, st, "home")
	require.Equal(t, 0, s.sel)
}

func TestNarrowOpensTheDetail(t *testing.T) {
	st := state(false)
	s := New()
	press(s, st, "end")
	require.Equal(t, "details", keys(s.Actions(st))["enter"])
	_, cmd := s.Update(core.Press("enter"), st)
	require.Nil(t, cmd, "the first enter only opens it")
	acts := keys(s.Actions(st))
	require.Equal(t, "back", acts["esc"])
	require.Equal(t, "next", acts["↑↓"])
	require.Equal(t, "try now", acts["enter"])
	s.View(st, 94, 26)

	press(s, st, "esc")
	require.False(t, s.open)
	s.Update(core.Click{ID: rowID(entry{t: moving("a.jpg")})}, st)
	require.Equal(t, "a.jpg", name(s, st), "a click moves the selection")
	require.True(t, s.open)
	s.Update(core.SwitchTab{Tab: 1}, st)
	require.False(t, s.open, "leaving the tab closes it")
}

func TestMoveHintOnlyWithSomewhereToGo(t *testing.T) {
	st := state(true)
	st.Act = api.Activity{Earlier: []api.Transfer{failed("c.jpg")}}
	s := New()
	require.NotContains(t, keys(s.Actions(st)), "↑↓")
	st.Act = api.Activity{}
	require.Empty(t, s.Actions(st))
}
