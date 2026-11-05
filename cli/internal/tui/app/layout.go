package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	bodyX, bodyY = 3, 4
	windowY      = 4
)

func (a *App) View() tea.View {
	v := tea.NewView(a.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (a *App) render() string {
	w, h := a.st.W, a.st.H
	if w == 0 || h == 0 {
		return ""
	}
	p := ui.P()
	if a.small() {
		a.hits = nil
		msg := "Make the window a bit bigger"
		return strings.Repeat("\n", h/2) + strings.Repeat(" ", max(0, (w-ui.Width(msg))/2)) + ui.Paint(msg, p.Muted, nil, false)
	}
	var passes []ui.Pass
	var acts []core.Action
	switch {
	case !a.st.Connected:
		passes = append(passes, ui.Pass{Layer: a.down(w, h)})
		acts = []core.Action{{Key: "enter", Label: "start dokky"}, {Key: "q", Label: "quit"}}
	case a.gate != nil:
		passes = append(passes, ui.Pass{Layer: a.gate.View(a.st, w, h)})
		acts = a.gate.Actions(a.st)
	default:
		base := lipgloss.NewLayer("").AddLayers(
			ui.Header(w, a.st.T, tabNames, a.active, a.account()),
			a.tabs[a.active].View(a.st, w-2*bodyX, h-bodyY-3).X(bodyX).Y(bodyY),
		)
		passes = append(passes, ui.Pass{Layer: base})
		acts = append(a.tabs[a.active].Actions(a.st), core.Action{Key: "q", Label: "quit"})
	}
	for i, win := range a.windows {
		// windows stay clear of the footer row
		l := win.View(a.st, w-4, h-4)
		b := lipgloss.NewCompositor(l).Bounds()
		x, y := (w-b.Dx())/2, max(1, min(windowY, (h-3-b.Dy())/2))
		passes = append(passes, ui.Pass{Layer: lipgloss.NewLayer("").AddLayers(l).X(x).Y(y), Dim: true, Modal: true})
		if i == len(a.windows)-1 {
			acts = win.Actions(a.st)
		}
	}
	passes = append(passes, ui.Pass{Layer: ui.Footer(w, acts, "v"+buildinfo.Version).Y(h - 2)})
	if a.toast != nil && a.toast.Live(a.st.Now) {
		passes = append(passes, ui.Pass{Layer: a.toast.Layer(w, h-4, a.st.Now)})
	}
	out, hits := ui.Compose(w, h, passes...)
	a.hits = hits
	return out
}

// the live chip at the top right: what dokky is doing, else whether anything needs you
func (a *App) account() string {
	p, st := ui.P(), a.st
	acct := st.Snap.Account
	spin := func(label string) string {
		return ui.L(nil).B(ui.Spin(st.T), p.Accent).Sp(1).Raw(label).String()
	}
	uploads := 0
	for _, d := range st.Snap.Drives {
		uploads += d.Uploads
	}
	n, bad := st.Problems()
	switch {
	case acct.Pending:
		return spin(ui.Paint("Signing in", p.Accent, nil, false))
	case st.Act.Down > 0 || st.Act.Up > 0:
		return spin(ui.L(nil).T(ui.G.Down+" "+core.Speed(st.Act.Down), p.Muted).Sp(2).T(ui.G.Up+" "+core.Speed(st.Act.Up), p.Muted).String())
	case uploads > 0:
		return spin(ui.Paint("Uploading "+core.Count(uploads, "file"), p.Muted, nil, false))
	case n > 0 && bad:
		return ui.L(nil).B(ui.G.Cross, p.Coral).Sp(1).T(core.Count(n, "problem"), p.Coral).String()
	case n > 0:
		return ui.L(nil).B(ui.G.Bang, p.Buoy).Sp(1).T(core.Count(n, "problem"), p.Buoy).String()
	case acct.SignedOut:
		return ui.L(nil).B(ui.G.Bang, p.Buoy).Sp(1).T("Signed out", p.Buoy).String()
	case acct.Offline:
		return ui.L(nil).B(ui.G.Dash, p.Faint).Sp(1).T("Offline", p.Faint).String()
	case st.Checking:
		return spin(ui.Paint("Checking", p.Muted, nil, false))
	}
	return ui.L(nil).B(ui.G.Check, p.Kelp).Sp(1).T("Healthy", p.Muted).String()
}

// calm full screen while the daemon is away
func (a *App) down(w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	title := ui.Paint("dokky isn't running", p.Text, nil, true)
	sub := ui.Paint("Start it and your drives come back.", p.Muted, nil, false)
	if a.starting {
		title = ui.L(nil).B(ui.Spin(a.st.T), p.Accent).Sp(1).B("Starting dokky", p.Text).String()
		sub = ""
	}
	y := h/2 - 2
	b.Put((w-ui.Width(title))/2, y, title)
	b.Put((w-ui.Width(sub))/2, y+1, sub)
	return b.Layer()
}
