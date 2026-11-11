package confirm

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

// the messages a command sends, for commands that only send messages
func sent(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, sent(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func newConfirm(ran *bool) *Confirm {
	c := &Confirm{Question: "Remove it?", Do: "Remove", Run: func(*core.State) tea.Cmd {
		*ran = true
		return core.Say("Removed")
	}}
	Open(c)
	return c
}

func TestConfirmTabWalksTheBar(t *testing.T) {
	st := &core.State{}
	ran := false
	c := newConfirm(&ran)
	require.Equal(t, 0, c.window(st, 80, 24).Active, "a destructive question starts on Cancel")
	require.Equal(t, core.Action{Key: "enter", Label: "cancel"}, c.Actions(st)[0])

	c.Update(core.Press("down"), st)
	require.Equal(t, 1, c.window(st, 80, 24).Active)
	require.Equal(t, "remove", c.Actions(st)[0].Label, "enter says what it does now")
	c.Update(core.Press("up"), st)
	require.Equal(t, 0, c.window(st, 80, 24).Active)
	c.Update(core.Press("up"), st)
	require.Equal(t, 1, c.window(st, 80, 24).Active)

	_, cmd := c.Update(core.Press("enter"), st)
	require.True(t, ran)
	require.Contains(t, sent(cmd), core.CloseWindow{Screen: c})
	require.Contains(t, sent(cmd), core.Toast{Text: "Removed"})
}

func TestConfirmCancelIsSafe(t *testing.T) {
	st := &core.State{}
	ran := false
	c := newConfirm(&ran)
	_, cmd := c.Update(core.Press("space"), st)
	require.False(t, ran, "space on Cancel cancels")
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: c}}, sent(cmd))

	_, cmd = c.Update(core.Press("esc"), st)
	require.False(t, ran)
	require.Equal(t, []tea.Msg{core.CloseWindow{Screen: c}}, sent(cmd))
}

func TestConfirmPositiveStartsOnTheAction(t *testing.T) {
	st := &core.State{}
	c := &Confirm{Question: "Save?", Do: "Save", Positive: true, Run: func(*core.State) tea.Cmd { return nil }}
	Open(c)
	require.Equal(t, 1, c.window(st, 80, 24).Active)
	require.Equal(t, core.Action{Key: "enter", Label: "save", Accent: true}, c.Actions(st)[0])
}

func TestConfirmClickFocusesWhatItHit(t *testing.T) {
	st := &core.State{}
	ran := false
	c := newConfirm(&ran)
	c.Update(core.Click{ID: "btn:do"}, st)
	require.True(t, ran)
	require.Equal(t, 1, c.active)
}
