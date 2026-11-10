package devices

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/stretchr/testify/require"
)

func keys(acts []core.Action) []string {
	var out []string
	for _, a := range acts {
		out = append(out, a.Key)
	}
	return out
}

// a Devices window that has loaded this device and one other that is online
func loaded(t *testing.T) (*Devices, *core.State) {
	t.Helper()
	now := time.Now()
	st := &core.State{Now: now, Snap: api.State{Account: api.Account{LoggedIn: true, Email: "you@example.com"}}}
	s := &Devices{}
	s.load(st)
	s.Update(core.Reply{Req: s.req, Val: []api.Device{
		{ID: "other", Name: "studio.local", LastSeenAt: now},
		{ID: "self", Name: "laptop.local", Current: true, LastSeenAt: now},
	}}, st)
	return s, st
}

// the window a command opens
func opened(t *testing.T, cmd tea.Cmd) *confirm.Confirm {
	t.Helper()
	require.NotNil(t, cmd)
	w, ok := cmd().(core.OpenWindow)
	require.True(t, ok)
	c, ok := w.Screen.(*confirm.Confirm)
	require.True(t, ok)
	return c
}

func TestDevicesTabWalksCardsThenButtons(t *testing.T) {
	s, st := loaded(t)
	require.Equal(t, "row:self", s.at, "this device comes first and has focus")
	require.Equal(t, -1, s.window(st, 80, 40).Active, "no button is lit while a card has focus")
	require.Equal(t, []string{"enter", "↑↓", "esc"}, keys(s.Actions(st)))
	require.Equal(t, core.Action{Key: "enter", Label: "rename"}, s.Actions(st)[0])

	var seen []string
	for range 4 {
		s.Update(core.Press("down"), st)
		seen = append(seen, s.at)
	}
	require.Equal(t, []string{"row:other", "btn:rename", "btn:remove", "row:self"}, seen)

	s.Update(core.Press("up"), st)
	require.Equal(t, "btn:remove", s.at)
	require.Equal(t, 0, s.sel, "the buttons act on the last card focus left")
	require.Equal(t, 1, s.window(st, 80, 40).Active)
	require.Equal(t, core.Action{Key: "enter", Label: "remove", Danger: true}, s.Actions(st)[0])
	require.Contains(t, keys(s.Actions(st)), "← →")
	s.Update(core.Press("left"), st)
	require.Equal(t, "btn:rename", s.at)
}

func TestDevicesEnterRenames(t *testing.T) {
	s, st := loaded(t)
	s.Update(core.Press("enter"), st)
	require.True(t, s.Capturing(), "enter on a card renames it")
	win := s.window(st, 80, 40)
	require.Equal(t, "Save", win.Buttons[win.Active].Label)
	s.Update(core.Press("esc"), st)
	require.False(t, s.Capturing())
	require.Equal(t, "row:self", s.at)

	s.Update(core.Click{ID: "btn:rename"}, st)
	require.True(t, s.Capturing())
	require.Equal(t, "btn:rename", s.at, "a click moves focus to what it hit")
	s.Update(core.Click{ID: "btn:cancel"}, st)
	require.False(t, s.Capturing())
}

func TestDevicesRemoveOther(t *testing.T) {
	s, st := loaded(t)
	s.Update(core.Click{ID: "row:other"}, st)
	require.Equal(t, 1, s.sel)
	s.Update(core.Press("down"), st)
	s.Update(core.Press("down"), st)
	require.Equal(t, "btn:remove", s.at)
	_, cmd := s.Update(core.Press("enter"), st)
	c := opened(t, cmd)
	require.Equal(t, "Remove studio?", c.Question)
}

func TestDevicesRemoveSelfKeepsDevicesUnderTheQuestion(t *testing.T) {
	s, st := loaded(t)
	_, cmd := s.Update(core.Press("d"), st)
	require.Equal(t, "btn:remove", s.at, "the letter presses the button")
	c := opened(t, cmd)
	require.Equal(t, "Remove this device?", c.Question)

	// Cancel goes back to Devices, which is still open
	_, cmd = c.Update(core.Press("esc"), st)
	require.Equal(t, core.CloseWindow{Screen: c}, cmd())

	// Remove closes Devices too
	run, ok := c.Run(st)().(tea.BatchMsg)
	require.True(t, ok)
	require.Equal(t, core.CloseWindow{Screen: s}, run[0]())
}

func TestDevicesEmpty(t *testing.T) {
	st := &core.State{}
	s := &Devices{}
	require.Nil(t, s.Buttons(st))
	require.Equal(t, []string{"esc"}, keys(s.Actions(st)))
	_, cmd := s.Update(core.Press("enter"), st)
	require.Nil(t, cmd)
}

func TestDevicesOneDeviceSaysTab(t *testing.T) {
	now := time.Now()
	st := &core.State{Now: now, Snap: api.State{Account: api.Account{LoggedIn: true}}}
	s := &Devices{}
	s.load(st)
	s.Update(core.Reply{Req: s.req, Val: []api.Device{{ID: "self", Name: "laptop", Current: true, LastSeenAt: now}}}, st)
	require.Equal(t, []string{"enter", "↑↓", "esc"}, keys(s.Actions(st)))
}
