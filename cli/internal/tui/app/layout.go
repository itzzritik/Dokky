package app

import (
	"image"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
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
	var acts []core.Action
	keys := footerDash
	bw, bh := w-2*bodyX, h-bodyY-3
	switch {
	case a.updatingNow():
		l := a.updatingLayer(a.updating(), w, h)
		passes = append(passes, a.behind(l, w, h), ui.Pass{Layer: l})
		keys = footerNone
	case !a.st.Connected || a.removedNow():
		l := a.splashLayer(a.splash(), w, h)
		passes = append(passes, a.behind(l, w, h), ui.Pass{Layer: l})
		if a.failed {
			acts = []core.Action{{Key: "enter", Label: "start dokky"}}
		}
		keys = footerQuit
	case a.gate != nil:
		// Forged's gate has no footer: its card carries its own keys
		passes = append(passes, ui.Pass{Layer: a.gate.View(a.st, w, h)})
		keys = footerNone
	default:
		base := lipgloss.NewLayer("").AddLayers(
			ui.Header(w, a.tabNames(), a.active, a.account()),
			a.tabs[a.active].View(a.st, bw, bh).X(bodyX).Y(bodyY),
		)
		passes = append(passes, ui.Pass{Layer: base})
		a.mark = nil
		acts = a.tabs[a.active].Actions(a.st)
	}
	for i, win := range a.windows {
		if a.updatingNow() {
			break
		}
		if full(win) {
			passes = append(passes, ui.Pass{Layer: a.blank(w, h), Modal: true}, ui.Pass{Layer: win.View(a.st, w, h), Modal: true})
			if i == len(a.windows)-1 {
				acts, keys = nil, footerNone
			}
			continue
		}
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
			acts, keys = win.Actions(a.st), footerWindow
		}
	}
	// as in Forged, a window fades everything under it, the footer too
	foot := ui.Pass{Layer: a.footer(w, acts, keys).Y(h - 2)}
	at := slices.IndexFunc(passes, func(p ui.Pass) bool { return p.Dim })
	if at < 0 {
		at = len(passes)
	}
	passes = slices.Insert(passes, at, foot)
	if a.toast != nil && a.toast.Live(a.st.Now) && (!a.waiting() || a.toast.Now) {
		passes = append(passes, ui.Pass{Layer: a.toast.Layer(w, h-4, a.st.Now)})
	}
	out, hits, caret := ui.Compose(w, h, passes...)
	a.hits, a.caret = hits, caret
	return out
}

var moreKey = core.Action{Key: "m", Label: "More", Icon: ui.G.Icon.Details}

// the lock screen's contour map behind a full screen of its own, quiet behind its words and the footer
func (a *App) behind(l *lipgloss.Layer, w, h int) ui.Pass {
	return ui.Pass{Layer: vault.Backdrop().Behind(w, h, a.st.Now, ui.Ink(l, w, h), image.Rect(0, h-2, w, h-1))}
}

func full(s core.Screen) bool {
	f, ok := s.(core.Full)
	return ok && f.Full()
}

// what the footer's right side may hold
type footerKind int

const (
	footerDash   footerKind = iota // a tab: More, Update and Quit
	footerWindow                   // a window: More when its keys don't all fit
	footerQuit                     // a full screen of its own: Quit
	footerNone
)

// at most 4 keys of the screen on top, then More, Update and Quit as Forged has them
func (a *App) footer(w int, acts []core.Action, kind footerKind) *lipgloss.Layer {
	left := acts[:min(4, len(acts))]
	var right []core.Action
	_, open := a.top().(*menu)
	free := !a.capturing()
	switch kind {
	case footerDash, footerWindow:
		if !open && free && (kind == footerDash || len(acts) > len(left) || ui.HintsWidth(left, true) > w-6) {
			right = append(right, moreKey)
		}
		if kind == footerDash && free {
			right = append(right, a.globals()...)
		}
	case footerQuit:
		if free {
			right = append(right, core.Action{Key: "q", Label: "Quit"})
		}
	}
	return ui.Footer(w, left, right)
}

// the window lock hides everything under it
func (a *App) blank(w, h int) *lipgloss.Layer {
	row := ui.Paint(strings.Repeat(" ", w), nil, ui.P().Bg, false)
	return lipgloss.NewLayer(strings.TrimSuffix(strings.Repeat(row+"\n", h), "\n"))
}

// actions on every tab, in the footer's right side
func (a *App) globals() []core.Action {
	if a.st.UpdateReady() {
		return []core.Action{{Key: "u", Label: "Update", Accent: true, Icon: ui.G.Icon.Update}, quitKey}
	}
	return []core.Action{quitKey}
}

var (
	quitKey = core.Action{Key: "q", Label: "Quit", Icon: ui.G.Icon.Quit}
	lockKey = core.Action{Key: "l", Label: "Lock", Icon: ui.G.Icon.Lock}
)

// the menu ends with Update, Lock and Quit, as Forged's ends with Lock and Quit
func (a *App) menuGlobals() []core.Action {
	var acts []core.Action
	if a.st.UpdateReady() {
		acts = append(acts, core.Action{Key: "u", Label: "Update", Accent: true, Icon: ui.G.Icon.Update})
	}
	if a.canLock() {
		acts = append(acts, lockKey)
	}
	return append(acts, quitKey)
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
	case st.Snap.Vault.Need == api.NeedJoin && st.Snap.Vault.State == api.VaultUnlocked:
		return ui.L(nil).B(ui.G.Bang, p.Buoy).Sp(1).T("Sync paused", p.Buoy).String()
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
