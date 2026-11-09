package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
	"github.com/stretchr/testify/require"
)

func shown(w, h int) *App {
	a := New(core.Deps{})
	a.veil = false
	a.st.W, a.st.H = w, h
	return a
}

func screen(a *App) []string { return strings.Split(ansi.Strip(a.render()), "\n") }

func TestUpdatingStepsOnlyMoveForward(t *testing.T) {
	a := shown(100, 30)
	now := time.Now()
	a.restartOpen, a.pendingRestart, a.restartAt = true, "0.1.5", now

	a.st.Now = now.Add(time.Second)
	u := a.updating()
	require.Equal(t, 1, u.step)
	require.False(t, u.wait)
	require.Greater(t, u.fill, 1./3)
	require.Less(t, u.fill, 2./3)

	a.st.Now = now.Add(restartIn)
	u = a.updating()
	require.True(t, u.wait)
	require.InDelta(t, 2./3, u.fill, 1e-9)

	// the window after the reopen picks the bar up where the old one left it
	a = shown(100, 30)
	a.carry = &carry{from: "0.1.4", to: "0.1.5"}
	u = a.updating()
	require.Equal(t, updating{from: "0.1.4", to: "0.1.5", step: 2, fill: 2. / 3, wait: true}, u)

	a.carry.back = a.st.Now
	a.st.Now = a.carry.back.Add(fillIn)
	u = a.updating()
	require.Equal(t, len(updateSteps), u.step)
	require.InDelta(t, 1, u.fill, 1e-9)
}

func TestUpdatingCopyPerStep(t *testing.T) {
	a := shown(100, 30)
	a.restartOpen, a.pendingRestart, a.restartAt, a.st.Now = true, "0.1.5", time.Now(), time.Now()
	text := strings.Join(screen(a), "\n")
	require.Contains(t, text, "Updating")
	require.Contains(t, text, "v"+buildinfo.Version+" "+ui.G.To+" v0.1.5")
	require.Contains(t, text, ui.G.Check+"  Installed")
	require.Contains(t, text, "Restarting dokky")
	require.Contains(t, text, ui.G.Ring+"  Reconnecting your drives")
	require.Contains(t, text, "Your drives come back on their own.")
	require.NotContains(t, text, "Restarting in")

	a = shown(100, 30)
	a.carry = &carry{from: "0.1.4", to: "0.1.5", back: a.st.Now}
	text = strings.Join(screen(a), "\n")
	require.Contains(t, text, "Updated")
	require.Contains(t, text, ui.G.Check+"  Restarted")
	require.Contains(t, text, ui.G.Check+"  Reconnected")
	require.Contains(t, text, "You're all set.")
}

func TestUpdatingCarriesAcrossReopen(t *testing.T) {
	t.Setenv(updatingEnv, "")
	a := shown(100, 30)
	a.pendingRestart = "0.1.5"
	_ = a.onRestartNow()
	require.Equal(t, buildinfo.Version+" 0.1.5", os.Getenv(updatingEnv))

	// a repair's reopen comes through here too, so only the handed over update carries on
	require.Nil(t, New(core.Deps{}).carry)
	next := New(core.Deps{Reopened: true})
	require.Equal(t, &carry{from: buildinfo.Version, to: "0.1.5"}, next.carry)
	require.False(t, next.veil)
	require.Empty(t, os.Getenv(updatingEnv))
	require.Nil(t, New(core.Deps{Reopened: true}).carry)
}

func TestUpdatingHidesReconnectingUntilNewVersion(t *testing.T) {
	a := shown(100, 30)
	a.carry = &carry{from: "0.1.4", to: "0.1.5"}
	for _, set := range []func(){
		func() { a.starting = true },
		func() { a.starting, a.lostAt = false, a.st.Now },
		func() { a.lostAt, a.failed = time.Time{}, true },
	} {
		set()
		text := strings.Join(screen(a), "\n")
		require.Contains(t, text, "Reconnecting your drives")
		for _, other := range []string{"Starting dokky", "This takes a few seconds.", "dokky isn't running", "start dokky"} {
			require.NotContains(t, text, other)
		}
	}

	// the old service answering on its way out is not the update done
	a.st.Connected = true
	a.post(core.Toast{Text: "Fixed 1 problem"})
	chosen := api.Settings{Chosen: true}
	_ = a.setSnap(api.State{Version: "0.1.4", Settings: chosen})
	require.True(t, a.carry.back.IsZero())
	require.Nil(t, a.toast)

	require.NotNil(t, a.setSnap(api.State{Version: "0.1.5", Settings: chosen}))
	require.False(t, a.carry.back.IsZero())
	require.True(t, a.updatingNow())

	_ = a.onSettled()
	require.False(t, a.updatingNow())
	require.Equal(t, "Updated to v0.1.5", a.toast.Text)
	require.Equal(t, []core.Toast{{Text: "Fixed 1 problem"}}, a.pending)
}

func TestUpdatingGivesUpWhenOverdue(t *testing.T) {
	a := shown(100, 30)
	a.carry, a.starting = &carry{from: "0.1.4", to: "0.1.5"}, true
	_ = a.onOverdue()
	require.Nil(t, a.carry)
	require.Contains(t, strings.Join(screen(a), "\n"), "Starting dokky")

	// already back: the finish runs out as usual
	a.carry = &carry{to: "0.1.5", back: a.st.Now}
	_ = a.onOverdue()
	require.NotNil(t, a.carry)
}

func TestUpdatingWaitsForUploads(t *testing.T) {
	a := shown(100, 30)
	a.pendingRestart = "0.1.5"
	a.st.Snap.Drives = []api.Drive{{ID: "a", Uploads: 2}}
	require.Nil(t, a.restartWhenIdle())
	require.False(t, a.updatingNow())
	require.Equal(t, "Update ready", a.st.Updating)

	a.st.Snap.Drives[0].Uploads = 0
	require.NotNil(t, a.restartWhenIdle())
	require.True(t, a.restartOpen)
	require.Empty(t, a.st.Updating)
}

func TestUpdatingFitsSmallWindows(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 20}, {40, 14}} {
		w, h := size[0], size[1]
		a := shown(w, h)
		a.carry = &carry{from: "0.1.4", to: "0.1.5"}
		rows := screen(a)
		require.Len(t, rows, h)
		sub, bar := -1, ""
		for i, r := range rows {
			if strings.Contains(r, "Your drives come back on their own.") {
				sub = i
			}
			if strings.Contains(r, ui.G.Heavy+ui.G.Heavy) {
				bar = r
			}
		}
		require.Positive(t, sub, "%dx%d", w, h)
		require.Less(t, sub, h-2, "%dx%d: clear of the footer", w, h)
		require.Equal(t, min(36, w-8), strings.Count(bar, ui.G.Heavy), "%dx%d", w, h)
		require.Contains(t, strings.Join(rows, "\n"), "Reconnecting your drives")
	}
	a := shown(39, 14)
	a.carry = &carry{to: "0.1.5"}
	require.Contains(t, strings.Join(screen(a), "\n"), "Make the window a bit bigger")
}
