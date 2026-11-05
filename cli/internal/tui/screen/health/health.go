package health

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	twoCol   = 110
	listW    = 46
	nameW    = 24
	bannerH  = 3
	shortBox = 18
)

type Health struct {
	sel   int
	moved bool
}

func New() *Health { return &Health{} }

// live checks from the daemon's state, then the computer's own
func checks(st *core.State) []core.Check {
	out := []core.Check{drives(st), sync(st)}
	if len(st.Checks) == 0 && st.Checking {
		return append(out, core.Check{Name: "This computer", Status: "Checking", Detail: "Looking at the file system driver, the background service, the keychain, the network and free space.", Tone: core.Busy})
	}
	return append(out, st.Checks...)
}

func drives(st *core.State) core.Check {
	c := core.Check{Name: "Drives", Tone: core.Good}
	if len(st.Snap.Drives) == 0 {
		c.Status, c.Detail = "No drives yet", "Add a drive from Overview."
		return c
	}
	on := 0
	var lines, failed []string
	for _, d := range st.Snap.Drives {
		s := ui.DriveStatus(d, st.T)
		lines = append(lines, d.Name+": "+s.Words)
		switch d.State {
		case api.Connected:
			on++
		case api.Failed:
			failed = append(failed, d.Name)
		case api.Reconnect:
			if c.Tone == core.Good {
				c.Tone = core.Warn
			}
		}
	}
	c.Status = core.Count(on, "drive") + " connected"
	if len(failed) > 0 {
		c.Tone, c.Status = core.Bad, strings.Join(failed, ", ")+" can't connect"
	}
	c.Detail = strings.Join(lines, "\n")
	return c
}

func sync(st *core.State) core.Check {
	a := st.Snap.Account
	switch {
	case !a.LoggedIn:
		return core.Check{Name: "Sync", Status: "This computer only", Detail: "Sign in from Settings to bring your drives to every computer.", Tone: core.Good}
	case a.Pending:
		return core.Check{Name: "Sync", Status: "Signing in", Detail: "Finishing the sign-in.", Tone: core.Busy}
	case a.SignedOut:
		return core.Check{Name: "Sync", Status: "Signed out", Detail: "This computer was signed out of your account. Sign in again from Settings.", Tone: core.Warn}
	case a.Offline:
		return core.Check{Name: "Sync", Status: "Offline", Detail: "dokky.ritik.me can't be reached. Your drives keep working and sync when it is back.", Tone: core.Warn}
	}
	return core.Check{Name: "Sync", Status: "Synced " + core.Ago(a.LastSyncAt, st.Now), Detail: "Signed in as " + a.Email + ". Drives and settings follow you to every computer.", Tone: core.Good}
}

// the first problem is shown until the person moves
func (h *Health) selected(cs []core.Check) int {
	if h.moved {
		return max(0, min(h.sel, len(cs)-1))
	}
	for i, c := range cs {
		if c.Tone == core.Warn || c.Tone == core.Bad {
			return i
		}
	}
	return 0
}

func (h *Health) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if c, ok := msg.(core.Click); ok && strings.HasPrefix(c.ID, "check:") {
			for i, x := range checks(st) {
				if c.ID == "check:"+x.Name {
					h.sel, h.moved = i, true
				}
			}
		}
		return h, nil
	}
	cs := checks(st)
	switch k.String() {
	case "up":
		h.sel, h.moved = max(0, h.selected(cs)-1), true
	case "down":
		h.sel, h.moved = min(len(cs)-1, h.selected(cs)+1), true
	case "r":
		return h, core.Send(core.RunChecks{})
	case "c":
		return h, tea.Batch(tea.SetClipboard(report(st, cs)), core.Say("Report copied"))
	}
	return h, nil
}

func report(st *core.State, cs []core.Check) string {
	var b strings.Builder
	b.WriteString("dokky " + st.Snap.Version + " health\n\n")
	for _, c := range cs {
		mark := map[core.Tone]string{core.Good: "ok", core.Warn: "warn", core.Bad: "fail", core.Busy: "busy"}[c.Tone]
		b.WriteString(mark + "  " + c.Name + ": " + c.Status + "\n")
		for _, l := range strings.Split(c.Detail, "\n") {
			b.WriteString("      " + l + "\n")
		}
	}
	return b.String()
}

func (h *Health) Actions(st *core.State) []core.Action {
	acts := []core.Action{{Key: "↑↓", Label: "move"}, {Key: "c", Label: "copy report"}}
	if !st.Checking {
		acts = append(acts, core.Action{Key: "r", Label: "check again"})
	}
	return acts
}

func (h *Health) Animating(st *core.State) bool { return st.Checking && !ui.Reduced() }

func tone(t core.Tone) (string, ui.Status) {
	p := ui.P()
	switch t {
	case core.Warn:
		return ui.G.Bang, ui.Status{Fg: p.Buoy, WordsFg: p.Buoy}
	case core.Bad:
		return ui.G.Cross, ui.Status{Fg: p.Coral, WordsFg: p.Coral}
	case core.Busy:
		return "", ui.Status{Fg: p.Accent, WordsFg: p.Muted}
	}
	return ui.G.Check, ui.Status{Fg: p.Kelp, WordsFg: p.Muted}
}

func (h *Health) View(st *core.State, w, ht int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	cs := checks(st)
	sel := h.selected(cs)
	bh := 1
	if ht >= shortBox+bannerH {
		bh = bannerH
	}
	b.Lines(0, 0, banner(st, cs, w, bh))
	y := bh + 1
	room := ht - y
	if room <= 0 {
		return b.Layer()
	}
	lw := w
	if w >= twoCol {
		lw = listW
	}
	start := max(0, sel-room+1)
	for i := start; i < len(cs) && i-start < room; i++ {
		b.Hit(0, y+i-start, "check:"+cs[i].Name, row(st, cs[i], lw, i == sel))
	}
	c := cs[sel]
	if w >= twoCol {
		dw := w - lw - 4
		rule := make([]string, room)
		for i := range rule {
			rule[i] = ui.Paint(ui.G.V, p.Line, nil, false)
		}
		b.Lines(lw+1, y, rule)
		b.Put(lw+3, y, ui.Paint(core.Fit(c.Name, dw), p.Text, nil, true))
		b.Lines(lw+3, y+2, wrap(c.Detail, dw, room-2))
		return b.Layer()
	}
	if n := min(len(cs), room); room-n >= 4 {
		dy := y + n + 1
		b.Put(0, dy, ui.Paint(strings.Repeat(ui.G.H, w), p.Line, nil, false))
		b.Put(3, dy+1, ui.Paint(core.Fit(c.Name, w-6), p.Text, nil, true))
		b.Lines(3, dy+2, wrap(c.Detail, w-6, room-n-3))
	}
	return b.Layer()
}

func wrap(s string, w, limit int) []string {
	p := ui.P()
	var out []string
	for _, para := range strings.Split(s, "\n") {
		for _, l := range strings.Split(ansi.Wordwrap(core.Safe(para), w, ""), "\n") {
			out = append(out, ui.Paint(l, p.Muted, nil, false))
		}
	}
	return out[:min(len(out), max(0, limit))]
}

func row(st *core.State, c core.Check, w int, on bool) string {
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		g, s := tone(c.Tone)
		if c.Tone == core.Busy {
			g = ui.Spin(st.T)
		}
		name := ui.L(bg).B(g, s.Fg).Sp(1)
		if on {
			name.B(c.Name, ui.P().Text)
		} else {
			name.T(c.Name, ui.P().Text)
		}
		return ui.L(bg).Raw(name.Pad(nameW)).T(c.Status, s.WordsFg).Pad(iw)
	})
}

// one tinted band when something needs you, a calm line when all is well
func banner(st *core.State, cs []core.Check, w, h int) []string {
	p := ui.P()
	n, bad := 0, false
	for _, c := range cs {
		switch c.Tone {
		case core.Bad:
			n, bad = n+1, true
		case core.Warn:
			n++
		}
	}
	lines := make([]string, h)
	mid := h / 2
	if n == 0 {
		text := ui.L(nil).Sp(2).B(ui.G.Check, p.Kelp).Sp(1).T("Everything is working", p.Text)
		right := ui.L(nil)
		switch {
		case st.Checking:
			right.T(ui.Spin(st.T)+" Checking", p.Muted)
		case !st.Checked.IsZero():
			right.T("Checked "+core.Ago(st.Checked, st.Now), p.Muted)
		}
		lines[mid] = text.Right(w-1, right)
		return lines
	}
	c, glyph := p.Buoy, ui.G.Bang
	if bad {
		c, glyph = p.Coral, ui.G.Cross
	}
	bg := ui.Mix(p.Bg, c, .12)
	verb := " need attention."
	if n == 1 {
		verb = " needs attention."
	}
	for i := range lines {
		l := ui.L(bg).Raw(ui.Paint(ui.G.Bar, c, bg, false))
		if i == mid {
			l.Sp(2).B(glyph, c).Sp(1).T(core.Count(n, "problem")+verb, p.Text)
		}
		lines[i] = l.Pad(w)
	}
	return lines
}
