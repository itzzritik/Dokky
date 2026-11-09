package app

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/tui/brand"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// updatingEnv hands the update to the window after the reopen as "from to", so it carries Updating on instead of starting over at Connecting
const updatingEnv = "DOKKY_UPDATING"

const (
	backWithin = 30 * time.Second       // the reopened window waits this long for the service, then shows what is really going on
	fillIn     = 450 * time.Millisecond // the bar's last third, once the service is back
	settleFor  = time.Second            // everything done stays up this long before the dashboard
)

// each step as it reads in flight and once done; the bar gives each a third
var updateSteps = [3][2]string{{"Installing", "Installed"}, {"Restarting dokky", "Restarted"}, {"Reconnecting your drives", "Reconnected"}}

// the update this window reopened into, kept on screen until the service runs it
type carry struct {
	from, to string
	back     time.Time // the service answered on the new dokky
}

// backWithin passed since the reopen
type overdue struct{}

// everything done had its moment
type settled struct{}

// the Updating moment, from the restart's grace in the old window until the service is back under the new one
type updating struct {
	from, to string
	step     int     // the step in flight; len(updateSteps) once dokky is back
	fill     float64 // only ever forward, across the reopen too
	wait     bool    // a step nothing can measure, so light runs along the bar
}

// the restart's grace in this window, or the update it reopened into; nothing else shows meanwhile
func (a *App) updatingNow() bool { return a.restartOpen || a.carry != nil }

func (a *App) updating() updating {
	if c := a.carry; c != nil {
		if c.back.IsZero() {
			return updating{from: c.from, to: c.to, step: 2, fill: 2. / 3, wait: true}
		}
		return updating{from: c.from, to: c.to, step: len(updateSteps), fill: ui.Ease(2./3, 1, c.back, a.st.Now, fillIn)}
	}
	// installed already, so the bar starts a third in and fills the second third as the restart nears
	return updating{from: buildinfo.Version, to: a.pendingRestart, step: 1,
		fill: ui.Ease(1./3, 2./3, a.restartAt, a.st.Now, restartIn), wait: a.st.Now.Sub(a.restartAt) >= restartIn}
}

// the window before the reopen was updating, so this one starts on Updating too
func carried() *carry {
	from, to, ok := strings.Cut(os.Getenv(updatingEnv), " ")
	if !ok || to == "" {
		return nil
	}
	// gone before anything else can reopen, so a later repair's reopen opens normally
	_ = os.Unsetenv(updatingEnv)
	return &carry{from: from, to: to}
}

// the service answered on the new dokky: the last third fills, then the dashboard takes over
func (a *App) arrived() tea.Cmd {
	c, v := a.carry, a.st.Snap.Version
	if c == nil || !c.back.IsZero() || !a.st.Connected || v != c.to && !buildinfo.Newer(v, c.to) {
		return nil
	}
	c.back = a.st.Now
	return tea.Tick(settleFor, func(time.Time) tea.Msg { return settled{} })
}

// still not back, so the usual Reconnecting or isn't running takes over and the update says nothing more
func (a *App) onOverdue() tea.Cmd {
	if a.carry == nil || !a.carry.back.IsZero() {
		return nil
	}
	a.carry = nil
	return a.nextToast()
}

// the notices held while Updating showed come after the one that says it worked
func (a *App) onSettled() tea.Cmd {
	if a.carry == nil {
		return nil
	}
	a.pending = append([]core.Toast{{Text: "Updated to v" + a.carry.to}}, a.pending...)
	a.carry = nil
	return a.nextToast()
}

// the mark, where the update goes, the bar, its steps and a calm line; the mark steps aside in a small window
func (a *App) updatingLayer(u updating, w, h int) *lipgloss.Layer {
	p, t := ui.P(), a.st.T
	b := ui.NewBox()
	center := func(y int, s string) { b.Put((w-ui.Width(s))/2, y, s) }
	// without the mark it is ten rows, sat a little above the middle and clear of the footer
	y := max(1, (h-13)/2)
	if w >= brand.W+4 && h >= 22 {
		y = max(1, h/2-9)
		b.Put((w-brand.W)/2, y, a.markView())
		y += 7
	}

	title, sub := "Updating", "Your drives come back on their own."
	if u.step == len(updateSteps) {
		title, sub = "Updated", "You're all set."
	}
	center(y, ui.Paint(title, p.Text, nil, true))
	ver := ui.L(nil)
	if u.from != "" {
		ver.T("v"+u.from, p.Muted).Sp(1).T(ui.G.To, p.Faint).Sp(1)
	}
	center(y+1, ver.B("v"+u.to, p.Accent).String())

	bw := min(36, w-8)
	b.Put((w-bw)/2, y+3, ui.FlowBar(bw, u.fill, t, u.wait))

	// the list keeps one left edge however its words change
	sw := 0
	for _, s := range updateSteps {
		sw = max(sw, ui.Width(s[0]))
	}
	x := (w - sw - 3) / 2
	for i, s := range updateSteps {
		l := ui.L(nil)
		switch {
		case i < u.step:
			l.B(ui.G.Check, p.Kelp).Sp(2).T(s[1], p.Muted)
		case i == u.step:
			l.B(ui.Spin(t), ui.SpinFg(t)).Sp(2).T(s[0], p.Text)
		default:
			l.T(ui.G.Ring, p.Faint).Sp(2).T(s[0], p.Faint)
		}
		b.Put(x, y+5+i, l.String())
	}
	center(y+9, ui.Paint(core.Fit(sub, w-4), p.Faint, nil, false))
	return b.Layer()
}
