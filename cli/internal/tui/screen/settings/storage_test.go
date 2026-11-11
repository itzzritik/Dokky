package settings

import (
	"testing"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/stretchr/testify/require"
)

func cached(n int64) *core.State {
	return &core.State{Snap: api.State{CacheBytes: n, Drives: []api.Drive{{ID: "d1", Name: "Photos", CacheBytes: n}}}}
}

func opensConfirm(t *testing.T, s *storage, k string, st *core.State) {
	t.Helper()
	_, cmd := s.Update(core.Press(k), st)
	require.NotNil(t, cmd, k)
	w, ok := cmd().(core.OpenWindow)
	require.True(t, ok, k)
	require.IsType(t, &confirm.Confirm{}, w.Screen, k)
}

func TestStorageButtons(t *testing.T) {
	st := cached(4 << 20)
	s := &storage{at: "btn:close"}
	win := s.window(st, 80, 24)
	require.Len(t, win.Buttons, 2)
	require.Equal(t, 0, win.Active, "Close has focus first")
	require.Equal(t, core.Action{Key: "enter", Label: "close"}, s.Actions(st)[0])

	s.Update(core.Press("right"), st)
	require.Equal(t, "btn:clear", s.at)
	require.Equal(t, 1, s.window(st, 80, 24).Active)
	require.Equal(t, core.Action{Key: "enter", Label: "clear", Danger: true}, s.Actions(st)[0])
	opensConfirm(t, s, "enter", st)

	s.Update(core.Press("down"), st)
	require.Equal(t, "btn:close", s.at, "tab wraps along the bar")
	s.Update(core.Press("up"), st)
	require.Equal(t, "btn:clear", s.at)
	s.Update(core.Press("left"), st)
	_, cmd := s.Update(core.Press("enter"), st)
	require.Equal(t, core.CloseWindow{Screen: s}, cmd())
}

func TestStorageClearShortcutAndClicks(t *testing.T) {
	st := cached(4 << 20)
	s := &storage{at: "btn:close"}
	opensConfirm(t, s, "c", st)
	require.Equal(t, "btn:clear", s.at, "c moves focus to the button it pressed")

	_, cmd := s.Update(core.Click{ID: "btn:close"}, st)
	require.Equal(t, "btn:close", s.at)
	require.Equal(t, core.CloseWindow{Screen: s}, cmd())
}

func TestStorageEmpty(t *testing.T) {
	s := &storage{at: "btn:clear"}
	empty := cached(0)
	require.Len(t, s.Buttons(empty), 1)
	require.NotContains(t, s.Actions(empty), core.Action{Key: "← →", Label: "choose"})
	_, cmd := s.Update(core.Press("c"), empty)
	require.Nil(t, cmd, "nothing to clear")
	_, cmd = s.Update(core.Press("enter"), empty)
	require.Equal(t, core.CloseWindow{Screen: s}, cmd(), "focus left with Clear")
}
