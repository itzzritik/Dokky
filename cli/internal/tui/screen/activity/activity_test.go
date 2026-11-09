package activity

import (
	"strconv"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func state(wide bool, items ...api.Event) *core.State {
	return &core.State{W: 120, H: 30, Wide: wide, Now: time.Now(), Log: api.Events{Items: items},
		Snap: api.State{Drives: []api.Drive{{ID: "d1", Name: "Photos"}}}}
}

func event(id, kind, target string) api.Event {
	return api.Event{ID: id, Kind: kind, Target: target, Name: "Photos", OK: true, At: time.Now()}
}

func press(s *Activity, st *core.State, keys ...string) {
	for _, k := range keys {
		s.Update(core.Press(k), st)
	}
}

func keys(acts []core.Action) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.Key] = a.Label
	}
	return out
}

func TestEnterGoesToTheDrive(t *testing.T) {
	st := state(true, event("e1", "drive.mounted", "d1"), event("e2", "device.added", "x"), event("e3", "drive.removed", "gone"))
	s := New()
	require.Equal(t, "show drive", keys(s.Actions(st))["enter"])
	_, cmd := s.Update(core.Press("enter"), st)
	require.Equal(t, core.SwitchTab{Tab: core.DrivesTab, Drive: "d1"}, cmd())

	press(s, st, "down")
	require.NotContains(t, keys(s.Actions(st)), "enter", "a device has no drive to show")
	press(s, st, "down")
	_, cmd = s.Update(core.Press("enter"), st)
	require.Nil(t, cmd, "a removed drive is gone")
}

func TestNarrowOpensTheDetailFirst(t *testing.T) {
	st := state(false, event("e1", "drive.mounted", "d1"), event("e2", "drive.unmounted", "d1"))
	s := New()
	_, cmd := s.Update(core.Press("enter"), st)
	require.Nil(t, cmd)
	acts := keys(s.Actions(st))
	require.Equal(t, "back", acts["esc"])
	require.Equal(t, "next", acts["↑↓"])
	require.Equal(t, "show drive", acts["enter"])
	_, cmd = s.Update(core.Press("enter"), st)
	require.Equal(t, core.SwitchTab{Tab: core.DrivesTab, Drive: "d1"}, cmd())
	s.Update(core.SwitchTab{Tab: core.DrivesTab, Drive: "d1"}, st)
	require.False(t, s.open, "leaving the tab closes it")
}

func TestHomeEndAndPages(t *testing.T) {
	var items []api.Event
	for i := range 50 {
		items = append(items, event("e"+strconv.Itoa(i), "drive.mounted", "d1"))
	}
	st := state(true, items...)
	s := New()
	s.View(st, 114, 20)
	press(s, st, "pgdown")
	require.Equal(t, "e17", s.selID)
	press(s, st, "end")
	require.Equal(t, "e49", s.selID)
	press(s, st, "pgup")
	require.Equal(t, "e32", s.selID)
	press(s, st, "home")
	require.Equal(t, "e0", s.selID)

	// new events come in on top; the selection stays on its event
	press(s, st, "down", "down")
	st.Log.Items = append([]api.Event{event("new", "drive.added", "d1")}, st.Log.Items...)
	e, _ := s.current(st)
	require.Equal(t, "e2", e.ID)
}
