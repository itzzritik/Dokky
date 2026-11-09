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
	a.caret = nil
	v := tea.NewView(a.render())
	if a.caret != nil {
		v.Cursor = &tea.Cursor{Position: tea.Position{X: a.caret.X, Y: a.caret.Y}, Shape: tea.CursorBar, Blink: true}
	}
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (a *App) render() string {
	w, h := a.st.W, a.st.H
	if w == 0 || h == 0 || a.veil {
		return ""
	}
	p := ui.P()
	if a.small() {
		a.hits = nil
		msg := "Make the window a bit bigger"
		return strings.Repeat("\n", h/2) + strings.Repeat(" ", max(0, (w-ui.Width(msg))/2)) + ui.Paint(msg, p.Muted, nil, false)
	}
	var passes []ui.Pass
	var left []core.Action
	right := []core.Action{{Key: "q", Label: "quit"}}
	var more *core.Action
	bw, bh := w-2*bodyX, h-bodyY-3
	switch {
	case a.updatingNow():
		passes = append(passes, ui.Pass{Layer: a.updatingLayer(a.updating(), w, h)})
		right = nil
	case !a.st.Connected || a.removedNow():
		passes = append(passes, ui.Pass{Layer: a.splashLayer(a.splash(), w, h)})
		if a.failed {
			left = []core.Action{{Key: "enter", Label: "start dokky"}}
		}
	case a.gate != nil:
		passes = append(passes, ui.Pass{Layer: a.gate.View(a.st, w, h)})
		left, right = a.gate.Actions(a.st), a.globals()
	default:
		base := lipgloss.NewLayer("").AddLayers(
			ui.Header(w, a.tabNames(), a.active, a.account()),
			a.tabs[a.active].View(a.st, bw, bh).X(bodyX).Y(bodyY),
		)
		passes = append(passes, ui.Pass{Layer: base})
		a.mark = nil
		left, right, more = a.tabs[a.active].Actions(a.st), a.globals(), &moreKey
		if a.capturing() {
			right, more = nil, nil
		}
	}
	for i, win := range a.windows {
		if a.updatingNow() {
			break
		}
		// windows stay clear of the footer row
		l := win.View(a.st, w-4, h-4)
		b := lipgloss.NewCompositor(l).Bounds()
		x, y := (w-b.Dx())/2, max(1, min(windowY, (h-3-b.Dy())/2))
		pl, placed := win.(core.Placer)
		if placed {
			x, y = pl.Place(bw, bh, b.Dx(), b.Dy())
			x, y = bodyX+x, bodyY+y
		}
		passes = append(passes, ui.Pass{Layer: lipgloss.NewLayer("").AddLayers(l).X(x).Y(y), Dim: !placed, Modal: true})
		if i == len(a.windows)-1 {
			left, right, more = win.Actions(a.st), nil, nil
			if _, open := win.(*menu); !open && !a.capturing() {
				more = &moreKey
			}
		}
	}
	version := "v" + buildinfo.Version
	if strings.Contains(buildinfo.Version, "dev") {
		version = "vDev"
	}
	passes = append(passes, ui.Pass{Layer: ui.Footer(w, left, right, more, version).Y(h - 2)})
	if a.toast != nil && a.toast.Live(a.st.Now) && (!a.waiting() || a.toast.Now) {
		passes = append(passes, ui.Pass{Layer: a.toast.Layer(w, h-4, a.st.Now)})
	}
	out, hits, caret := ui.Compose(w, h, passes...)
	a.hits, a.caret = hits, caret
	return out
}

var moreKey = core.Action{Key: "?", Label: "more"}

// actions on every tab, in the footer's right side and the menu
func (a *App) globals() []core.Action {
	if a.st.UpdateReady() {
		return []core.Action{{Key: "u", Label: "update", Accent: true}, {Key: "q", Label: "quit"}}
	}
	return []core.Action{{Key: "q", Label: "quit"}}
}

// the live chip at the top right: what dokky is doing, else whether anything needs you
func (a *App) account() string {
	p, st := ui.P(), a.st
	acct := st.Snap.Account
	spin := func(label string) string {
		return ui.L(nil).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).Raw(label).String()
	}
	uploads := 0
	for _, d := range st.Snap.Drives {
		uploads += d.Uploads
	}
	n, bad := st.Problems()
	switch {
	case acct.Pending:
		return spin(ui.Paint("Signing in", p.Accent, nil, false))
	case st.Updating == "Update ready":
		return ui.L(nil).B(ui.G.Check, p.Accent).Sp(1).T("Update ready", p.Muted).String()
	case st.Updating != "":
		return spin(ui.Paint(st.Updating, p.Muted, nil, false))
	case st.Fixing:
		return spin(ui.Paint("Fixing", p.Muted, nil, false))
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
