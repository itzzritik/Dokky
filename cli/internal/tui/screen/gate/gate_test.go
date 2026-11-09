package gate

import (
	"errors"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func keys(acts []core.Action) []string {
	var out []string
	for _, a := range acts {
		out = append(out, a.Key)
	}
	return out
}

// a sign in that has asked for its code, with the browser swapped for a recorder
func started(t *testing.T) (*SignIn, *core.State, *[]string) {
	var opened []string
	was := openURL
	openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	t.Cleanup(func() { openURL = was })
	st := &core.State{}
	s := &SignIn{}
	s.begin(st)
	s.at = s.primary()
	return s, st, &opened
}

var code = client.LoginStart{UserCode: "ABCD-EFGH", VerifyURL: "https://dokky.ritik.me/device", Interval: 5}

func TestSignInFocusesOpenBrowserOnceTheCodeArrives(t *testing.T) {
	s, st, opened := started(t)
	require.Equal(t, "btn:cancel", s.at, "only Cancel shows while it waits")
	require.Equal(t, []string{"enter", "esc"}, keys(s.Actions(st)))

	s.View(st, 80, 24) // drawing while only Cancel shows must not move focus
	s.Update(core.Reply{Req: s.req, Val: code}, st)
	win := s.window(st, 80, 24)
	require.Equal(t, "btn:open", s.at)
	require.Equal(t, 2, win.Active, "Open browser is lit, not Cancel")
	require.Equal(t, core.Action{Key: "enter", Label: "open browser", Accent: true}, s.Actions(st)[0])
	require.Equal(t, []string{"enter", "← →", "c", "o", "esc"}, keys(s.Actions(st)))

	_, cmd := s.Update(core.Press("enter"), st)
	require.NotNil(t, cmd)
	require.Nil(t, cmd(), "enter opened the browser")
	require.Equal(t, []string{code.VerifyURL}, *opened)
	require.NotZero(t, s.req, "and the sign in goes on")
}

func TestSignInRetryFocus(t *testing.T) {
	s, st, _ := started(t)
	s.Update(core.Reply{Req: s.req, Err: errors.New("no network")}, st)
	require.Equal(t, "btn:retry", s.at, "Press enter to try again means Try again is lit")
	require.Equal(t, 1, s.window(st, 80, 24).Active)

	s.Update(core.Press("enter"), st)
	require.Equal(t, "waiting", s.status)
	require.Equal(t, "btn:cancel", s.at)
	s.Update(core.Reply{Req: s.req, Val: code}, st)
	require.Equal(t, "btn:open", s.at)

	s.Update(core.Reply{Req: s.req, Val: polled{LoginPoll: client.LoginPoll{Status: "expired"}}}, st)
	require.Equal(t, "btn:retry", s.at)
	_, cmd := s.Update(core.Press("enter"), st)
	require.NotNil(t, cmd)
	require.NotZero(t, s.req, "enter asked for a new code instead of cancelling")
}

func TestSignInTabAndClicks(t *testing.T) {
	s, st, _ := started(t)
	s.Update(core.Reply{Req: s.req, Val: code}, st)
	var seen []string
	for range 3 {
		s.Update(core.Press("tab"), st)
		seen = append(seen, s.at)
	}
	require.Equal(t, []string{"btn:cancel", "btn:copy", "btn:open"}, seen)
	s.Update(core.Press("shift+tab"), st)
	require.Equal(t, "btn:copy", s.at)

	s.Update(core.Click{ID: "btn:open"}, st)
	require.Equal(t, "btn:open", s.at, "a click moves focus to what it hit")
	_, cmd := s.Update(core.Press("c"), st)
	require.Equal(t, "btn:copy", s.at)
	require.NotNil(t, cmd)

	_, cmd = s.Update(core.Press("esc"), st)
	require.Equal(t, core.CloseWindow{Screen: s}, cmd())
}

func TestGateTabFlips(t *testing.T) {
	st := &core.State{}
	g := New(time.Now())
	g.Update(core.Press("tab"), st)
	require.Equal(t, 1, g.sel)
	g.Update(core.Press("shift+tab"), st)
	require.Equal(t, 0, g.sel)
	g.Update(core.Press("shift+tab"), st)
	require.Equal(t, 1, g.sel)
}

func clashList(n int) []api.Clash {
	var out []api.Clash
	for i := range n {
		out = append(out, api.Clash{Kind: "drive", Name: string(rune('A' + i))})
	}
	return out
}

func TestClashesOneThingLit(t *testing.T) {
	st := &core.State{}
	c := NewClashes(clashList(2))
	require.Equal(t, "chip:0", c.at)
	require.Equal(t, -1, c.window(st, 80, 24).Active, "Continue is not lit while a row is")

	var seen []string
	for range 3 {
		c.Update(core.Press("tab"), st)
		seen = append(seen, c.at)
	}
	require.Equal(t, []string{"chip:1", "btn:continue", "chip:0"}, seen)
	c.Update(core.Press("up"), st)
	require.Equal(t, "btn:continue", c.at, "up from the first row wraps to the bar")
	require.Equal(t, 0, c.window(st, 80, 24).Active)
	require.Equal(t, core.Action{Key: "enter", Label: "continue", Accent: true}, c.Actions(st)[0])
}

func TestClashesSides(t *testing.T) {
	st := &core.State{}
	c := NewClashes(clashList(2))
	c.Update(core.Press("left"), st)
	require.Equal(t, 0, c.keep[0], "left is This computer")
	c.Update(core.Press("left"), st)
	require.Equal(t, 0, c.keep[0], "and stays there")
	c.Update(core.Press("right"), st)
	require.Equal(t, 1, c.keep[0], "right is Account")
	c.Update(core.Press("space"), st)
	require.Equal(t, 0, c.keep[0], "space flips")

	c.Update(core.Press("enter"), st)
	require.Equal(t, "chip:1", c.at, "enter on a row goes on to the next")
	require.Zero(t, c.req)
	c.Update(core.Press("enter"), st)
	require.Equal(t, "btn:continue", c.at)
	_, cmd := c.Update(core.Press("enter"), st)
	require.NotNil(t, cmd)
	require.True(t, c.busy(), "enter on the bar continues")
	require.Nil(t, c.Actions(st))
	require.Nil(t, c.Buttons(st))
}

func TestClashesClickFocusesTheRow(t *testing.T) {
	st := &core.State{}
	c := NewClashes(clashList(3))
	c.Update(core.Click{ID: "chip:2:0"}, st)
	require.Equal(t, "chip:2", c.at)
	require.Equal(t, 0, c.keep[2])
}

func TestClashesScrollToTheFocusedRow(t *testing.T) {
	st := &core.State{}
	c := NewClashes(clashList(12))
	const h = 12
	room := min(12, c.window(st, 80, h).Room(h))
	require.Less(t, room, 12, "the list is taller than the window")
	for range 11 {
		c.Update(core.Press("down"), st)
	}
	c.window(st, 80, h)
	require.Equal(t, 11-room+1, c.top, "the last row is drawn")
	for range 11 {
		c.Update(core.Press("up"), st)
	}
	c.window(st, 80, h)
	require.Equal(t, 0, c.top)
}
