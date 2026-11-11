package app

import (
	"os"

	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// followedEnv names the version this window already reopened for, so a binary that didn't change can't loop
const followedEnv = "DOKKY_FOLLOWED"

// one status line on the splash, for every moment dokky isn't on its dashboard
type splash struct {
	busy       bool
	words, sub string
	bar        float64 // above 0: a progress bar under the words
	button     string  // a primary button that presses enter
}

// Updating has a card of its own, so it never comes through here
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

// the splash's one button, Start dokky, which presses enter
const startID = "start"

// Forged's launch card: the words and a spinner on the stage every full screen shares, esc in the card
func (a *App) splashLayer(s splash, w, h int) *lipgloss.Layer {
	return a.onStage(w, h, func(win *ui.Window, cw int, bg color.Color) int {
		p := ui.P()
		line := ui.L(bg)
		if s.busy {
			line.B(ui.Spin(a.st.T), ui.SpinFg(a.st.T)).Sp(1)
		}
		rows := []string{centred(line.B(core.Fit(s.words, cw-2), p.Text).String(), cw, bg)}
		if s.sub != "" {
			rows = append(rows, centred(ui.L(bg).T(core.Fit(s.sub, cw), p.Muted).String(), cw, bg))
		}
		if s.bar > 0 {
			bw := min(36, cw)
			rows = append(rows, ui.L(bg).Pad(cw), centred(ui.ThinBar(bw, s.bar, a.st.T, p.Coral, bg), cw, bg))
		}
		if s.button != "" {
			win.Buttons, win.Active = []ui.Btn{{ID: startID, Label: s.button, Primary: true}}, 0
		}
		win.Hints = []core.Action{{Key: "esc", Label: "quit"}}
		for i, r := range rows {
			win.Body.Put(0, i, r)
		}
		return len(rows)
	})
}

// a card on the shared stage, its body filled by fill, which says how many rows it used
func (a *App) onStage(w, h int, fill func(win *ui.Window, cw int, bg color.Color) int) *lipgloss.Layer {
	return vault.Stage().Layer(w, h, a.st.Now, func(maxH int) *ui.Window {
		win := &ui.Window{W: ui.CardWidth(w), Body: ui.NewBox()}
		rows := fill(win, win.Inner(), ui.P().Window)
		win.Fit(rows, maxH)
		return win
	})
}

// s in the middle of a card row cw wide
func centred(s string, cw int, bg color.Color) string {
	return ui.L(bg).Sp(max(0, (cw-ui.Width(s))/2)).Raw(s).Pad(cw)
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
