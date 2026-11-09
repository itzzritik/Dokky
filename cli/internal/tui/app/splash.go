package app

import (
	"os"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/brand"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// followedEnv names the version this window already reopened for, so a binary that didn't change can't loop
const followedEnv = "DOKKY_FOLLOWED"

// the dokky mark with one status line under it, for every moment dokky isn't on its dashboard
type splash struct {
	busy       bool
	words, sub string
	bar        float64 // above 0: a progress bar under the words
	button     string  // a primary button that presses enter
}

// Updating has a layout of its own, so it never comes through here
func (a *App) splash() splash {
	switch {
	case a.removedNow():
		r := a.st.Snap.Removed
		sub := "Erasing its dokky data"
		if r.Waiting != "" {
			sub = "Finishing " + r.Waiting
		}
		return splash{busy: true, words: "This device was removed", sub: sub, bar: r.Progress}
	case a.starting:
		return splash{busy: true, words: "Starting dokky", sub: "Your drives come back in a moment."}
	case a.failed:
		return splash{words: "dokky isn't running", sub: "Start it and your drives come back.", button: "Start dokky"}
	case !a.lostAt.IsZero():
		return splash{busy: true, words: "Reconnecting", sub: "This takes a few seconds."}
	}
	return splash{busy: true, words: "Connecting"}
}

// the server removed this device; it only finishes its transfers and erases now
func (a *App) removedNow() bool { return a.st.Connected && a.st.Snap.Removed != nil }

func (a *App) splashLayer(s splash, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	y := max(1, h/2-9)
	b.Put((w-brand.W)/2, y, a.markView())
	center := func(y int, s string) { b.Put((w-ui.Width(s))/2, y, s) }
	line := ui.L(nil)
	if s.busy {
		line.B(ui.Spin(a.st.T), ui.SpinFg(a.st.T)).Sp(1)
	}
	center(y+7, line.B(s.words, p.Text).String())
	if s.sub != "" {
		center(y+8, ui.Paint(s.sub, p.Muted, nil, false))
	}
	switch {
	case s.bar > 0:
		bw := brand.W - 16
		b.Put((w-bw)/2, y+10, ui.ThinBar(bw, s.bar, a.st.T, p.Coral, p.Bg))
	case s.button != "":
		b.Add((w-ui.ButtonWidth(s.button))/2, y+10, ui.Button("key:enter", s.button, true, false, true))
	}
	return b.Layer()
}

// the splash's logo, made when the splash first shows so it fills with water then
func (a *App) markView() string {
	if a.mark == nil {
		a.mark = brand.NewMark(a.st.Now)
	}
	return a.mark.View(a.st.Now)
}

// the service moved to a newer dokky under this window, so the window reopens on it too
func (a *App) followService() {
	v := a.st.Snap.Version
	if a.pendingRestart != "" || buildinfo.Dev() || os.Getenv(followedEnv) == v || !buildinfo.Newer(v, buildinfo.Version) {
		return
	}
	_ = os.Setenv(followedEnv, v)
	a.pendingRestart = v
}
