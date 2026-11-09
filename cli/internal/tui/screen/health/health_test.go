package health

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/stretchr/testify/require"
)

func check(name string, tone core.Tone) core.Check {
	return core.Check{Name: name, Status: "x", Tone: tone}
}

func state(checks ...core.Check) *core.State {
	return &core.State{W: 120, H: 30, Now: time.Now(), Checked: time.Now(), Checks: checks,
		Snap: api.State{Account: api.Account{LoggedIn: true, Email: "you@example.com", LastSyncAt: time.Now()}}}
}

func press(h *Health, st *core.State, keys ...string) {
	for _, k := range keys {
		h.Update(core.Press(k), st)
	}
}

func name(h *Health, st *core.State) string {
	cs := Checks(st)
	return cs[h.selected(cs)].Name
}

func keys(acts []core.Action) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		out[a.Key] = a.Label
	}
	return out
}

func TestSelectionFollowsTheCheck(t *testing.T) {
	st := state(check("Background service", core.Good), check("Updates", core.Good), check("Cache", core.Good))
	h := New()
	press(h, st, "end")
	require.Equal(t, "Cache", name(h, st))
	// the checks run again and one more comes back above it
	st.Checks = append([]core.Check{check("macOS", core.Good)}, st.Checks...)
	require.Equal(t, "Cache", name(h, st))

	// the placeholder swaps for the real checks
	st.Checks, st.Checking = nil, true
	press(h, st, "home")
	require.Equal(t, "Sync", name(h, st))
	st.Checks, st.Checking = []core.Check{check("Background service", core.Good)}, false
	require.Equal(t, "Sync", name(h, st))
}

func TestHomeEndAndPages(t *testing.T) {
	var cs []core.Check
	for i := range 20 {
		cs = append(cs, check("Check "+string(rune('a'+i)), core.Good))
	}
	st := state(cs...)
	h := New()
	h.View(st, 120, 12)
	press(h, st, "pgdown")
	require.Equal(t, h.page, h.sel)
	press(h, st, "end")
	require.Equal(t, "Check t", name(h, st))
	press(h, st, "home")
	require.Equal(t, "Sync", name(h, st))
}

func TestEnterRepairsOneCheck(t *testing.T) {
	ran := false
	broken := check("Background service", core.Bad)
	broken.Repair = func() error { ran = true; return nil }
	st := state(broken, check("Updates", core.Good))
	h := New()
	h.View(st, 120, 30)
	require.Equal(t, "Background service", name(h, st), "the first problem is shown")
	require.Equal(t, "fix", keys(h.Actions(st))["enter"])

	_, cmd := h.Update(core.Press("enter"), st)
	require.True(t, st.Fixing)
	msg := cmd()
	require.True(t, ran)
	_, cmd = h.Update(msg, st)
	require.False(t, st.Fixing)
	require.NotNil(t, cmd)
}

func TestEnterOnSyncGoesToSettings(t *testing.T) {
	st := state(check("Updates", core.Good))
	st.Snap.Account.SignedOut = true
	h := New()
	h.View(st, 120, 30)
	require.Equal(t, "Sync", name(h, st))
	require.Equal(t, "open settings", keys(h.Actions(st))["enter"])
	_, cmd := h.Update(core.Press("enter"), st)
	require.Equal(t, core.SwitchTab{Tab: settingsTab}, cmd())
}

func TestNarrowOpensTheFullDetail(t *testing.T) {
	broken := check("Background service", core.Bad)
	broken.Repair = func() error { return nil }
	st := state(broken)
	h := New()
	h.View(st, 94, 26)
	require.Equal(t, "details", keys(h.Actions(st))["enter"])
	_, cmd := h.Update(core.Press("enter"), st)
	require.Nil(t, cmd, "the first enter only opens it")
	acts := keys(h.Actions(st))
	require.Equal(t, "back", acts["esc"])
	require.Equal(t, "fix", acts["enter"])
	h.View(st, 94, 26)

	press(h, st, "esc")
	require.False(t, h.open)
	h.Update(core.Click{ID: "check:Sync"}, st)
	require.Equal(t, "Sync", name(h, st), "a click moves the selection")
	require.True(t, h.open)
	h.Update(core.SwitchTab{Tab: 0}, st)
	require.False(t, h.open, "leaving the tab closes it")
}

func TestFailedRepairSaysSo(t *testing.T) {
	st := state()
	st.Fixing = true
	cmd := onRepaired(st, repaired{name: "Cache", err: errors.New("disk is full")})
	require.False(t, st.Fixing)
	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)
	toast, ok := batch[0]().(core.Toast)
	require.True(t, ok && toast.Bad)
}
