package remove

import (
	"testing"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

// a remove window that has heard back where the drive is mounted
func checked(st *core.State, check api.DriveCheck) *Remove {
	r := newRemove(api.Drive{ID: "d1", Name: "Photos"}, nil)
	r.load(st)
	r.Update(core.Reply{Req: r.req, Val: check}, st)
	return r
}

var elsewhere = api.DriveCheck{Usage: api.Usage{State: "idle"}, Others: []api.Peer{{Name: "studio.local", Active: true}}}

func keys(acts []core.Action) []string {
	var out []string
	for _, a := range acts {
		out = append(out, a.Key)
	}
	return out
}

func TestRemoveUnderstandIsAFocusStop(t *testing.T) {
	st := &core.State{}
	r := checked(st, elsewhere)
	require.Equal(t, "btn:cancel", r.at, "Cancel has focus first")
	require.Equal(t, 0, r.window(st, 80, 24).Active)
	require.Equal(t, []string{"enter", "↑↓", "← →", "esc"}, keys(r.Actions(st)))

	r.Update(core.Press("up"), st)
	require.Equal(t, sureID, r.at)
	require.Equal(t, -1, r.window(st, 80, 24).Active, "no button looks focused while the switch has it")
	require.Equal(t, core.Action{Key: "enter", Label: "tick"}, r.Actions(st)[0])
	require.NotContains(t, keys(r.Actions(st)), "← →", "the arrows only walk the bar")

	r.Update(core.Press("enter"), st)
	require.True(t, r.sure)
	require.Equal(t, "untick", r.Actions(st)[0].Label)
	r.Update(core.Press("space"), st)
	require.False(t, r.sure)
	r.Update(core.Press("space"), st)
	require.True(t, r.sure)

	r.Update(core.Press("down"), st)
	r.Update(core.Press("down"), st)
	require.Equal(t, "btn:do", r.at)
	require.Equal(t, 1, r.window(st, 80, 24).Active)
	require.Equal(t, core.Action{Key: "enter", Label: "remove anyway", Danger: true}, r.Actions(st)[0])
	_, cmd := r.Update(core.Press("enter"), st)
	require.NotNil(t, cmd, "ticked, so it removes")
	require.False(t, r.nudge)
}

func TestRemoveTabWalksEveryStop(t *testing.T) {
	st := &core.State{}
	r := checked(st, elsewhere)
	var seen []string
	for range 3 {
		r.Update(core.Press("down"), st)
		seen = append(seen, r.at)
	}
	require.Equal(t, []string{"btn:do", sureID, "btn:cancel"}, seen)
	r.Update(core.Press("right"), st)
	require.Equal(t, "btn:do", r.at)
	r.Update(core.Press("left"), st)
	require.Equal(t, "btn:cancel", r.at)
}

func TestRemoveAnywayClickFocusesIt(t *testing.T) {
	st := &core.State{}
	r := checked(st, elsewhere)
	_, cmd := r.Update(core.Click{ID: "btn:do"}, st)
	require.Nil(t, cmd)
	require.True(t, r.nudge, "it asks for the tick instead of failing silently")
	require.Equal(t, 1, r.window(st, 80, 24).Active, "the clicked button has focus")

	_, cmd = r.Update(core.Press("enter"), st)
	require.Nil(t, cmd, "the next enter does not cancel")
	require.True(t, r.nudge)

	r.Update(core.Click{ID: sureID}, st)
	require.True(t, r.sure)
	require.False(t, r.nudge)
	require.Equal(t, sureID, r.at)
}

func TestRemoveArrowsOnlyWithTwoButtons(t *testing.T) {
	st := &core.State{}
	r := checked(st, api.DriveCheck{Usage: api.Usage{State: "busy", Reason: "Files are open"}})
	require.Len(t, r.Buttons(st), 1)
	require.Equal(t, []string{"enter", "esc"}, keys(r.Actions(st)))
	_, cmd := r.Update(core.Press("enter"), st)
	require.Equal(t, core.CloseWindow{Screen: r}, cmd())
}

func TestRemoveHereStartsOnCancel(t *testing.T) {
	st := &core.State{}
	r := checked(st, api.DriveCheck{Usage: api.Usage{State: "idle"}})
	require.Equal(t, []string{"enter", "← →", "esc"}, keys(r.Actions(st)))
	_, cmd := r.Update(core.Press("enter"), st)
	require.Equal(t, core.CloseWindow{Screen: r}, cmd())
}
