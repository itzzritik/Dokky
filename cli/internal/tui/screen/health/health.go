package health

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	twoCol   = 110
	listW    = 46
	nameW    = 24
	bannerH  = 3
	shortBox = 18

	settingsTab = 5
)

type Health struct {
	sel     int
	selName string // checks swap in live, so the selection follows the name
	moved   bool
	open    bool // the full detail over the list, when it can't sit beside it
	split   bool // the detail sits beside the list, as last drawn
	page    int
}

func New() *Health { return &Health{} }

// dokky itself only: a drive that can't connect shows on its own card, not here
func Checks(st *core.State) []core.Check {
	out := []core.Check{sync(st)}
	if len(st.Checks) == 0 && st.Checking {
		return append(out, core.Check{Name: "This computer", Status: "Checking", Detail: "Looking at the background service, updates, the cache and the keychain.", Tone: core.Busy})
	}
	return append(out, st.Checks...)
}

func sync(st *core.State) core.Check {
	a := st.Snap.Account
	c := core.Check{Name: "Sync", Detail: "Your drives and settings follow you to every computer you sign in on.", Tone: core.Good}
	c.Facts = [][2]string{{"Account", a.Email}, {"Last synced", ""}, {"Server", "dokky.ritik.me"}}
	switch {
	case !a.LoggedIn:
		c.Status, c.Detail = "This computer only", "Sign in from Settings to bring your drives and settings to every computer."
		c.Facts[0][1] = "Not signed in"
		return c
	case a.Pending:
		c.Status, c.Tone = "Signing in", core.Busy
		return c
	case a.SignedOut:
		c.Status, c.Tone, c.Fix = "Signed out", core.Warn, "This computer was signed out of your account. Sign in again from Settings."
	case a.Offline:
		c.Status, c.Tone, c.Fix = "Offline", core.Warn, "Check your internet connection. Your drives keep working and sync when dokky.ritik.me is back."
	default:
		c.Status = "Synced " + core.Ago(a.LastSyncAt, st.Now)
	}
	if !a.LastSyncAt.IsZero() {
		c.Facts[1][1] = core.Ago(a.LastSyncAt, st.Now)
	}
	return c
}

// the first problem is shown until the person moves
func (h *Health) selected(cs []core.Check) int {
	if !h.moved {
		for i, c := range cs {
			if !c.Expected && (c.Tone == core.Warn || c.Tone == core.Bad) {
				return i
			}
		}
		return 0
	}
	for i, c := range cs {
		if c.Name == h.selName {
			h.sel = i
		}
	}
	h.sel = max(0, min(h.sel, len(cs)-1))
	h.selName = cs[h.sel].Name
	return h.sel
}

func (h *Health) choose(cs []core.Check, i int) {
	h.sel, h.selName, h.moved = i, cs[i].Name, true
}

type repaired struct {
	name   string
	err    error
	reopen bool
}

func (h *Health) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	cs := Checks(st)
	at := h.selected(cs)
	switch m := msg.(type) {
	case core.SwitchTab:
		h.open = false
	case repaired:
		return h, onRepaired(st, m)
	case core.Click:
		for i, x := range cs {
			if m.ID == "check:"+x.Name {
				h.choose(cs, i)
				h.open = !h.split
			}
		}
	case tea.KeyPressMsg:
		k := m.String()
		if i, moved := ui.Move(k, at, len(cs), h.page); moved {
			h.choose(cs, i)
			return h, nil
		}
		switch k {
		case "enter":
			if !h.split && !h.open {
				h.open = true
				return h, nil
			}
			return h, act(st, cs[at])
		case "esc":
			h.open = false
		case "r":
			return h, core.Send(core.RunChecks{})
		case "f":
			return h, core.Send(core.FixAll{})
		case "c":
			return h, tea.Batch(tea.SetClipboard(report(st, cs)), core.Say("Report copied"))
		}
	}
	return h, nil
}

// Fix all's repair for this one check, else where the person can sort it out
func act(st *core.State, c core.Check) tea.Cmd {
	switch {
	case fixable(st, c):
		st.Fixing = true
		run, name, reopen := c.Repair, c.Name, c.Reopen
		return func() tea.Msg { return repaired{name: name, err: run(), reopen: reopen} }
	case signIn(st, c):
		return core.Send(core.SwitchTab{Tab: settingsTab})
	}
	return nil
}

func actLabel(st *core.State, c core.Check) string {
	switch {
	case fixable(st, c):
		return "fix"
	case signIn(st, c):
		return "open settings"
	}
	return ""
}

func fixable(st *core.State, c core.Check) bool {
	return c.Repair != nil && (c.Tone == core.Warn || c.Tone == core.Bad) && !st.Fixing
}

// Sync is sorted out by signing in, which lives in Settings
func signIn(st *core.State, c core.Check) bool {
	a := st.Snap.Account
	return c.Name == "Sync" && (!a.LoggedIn || a.SignedOut)
}

// like Fix all when it ends: say how it went and look again
func onRepaired(st *core.State, m repaired) tea.Cmd {
	st.Fixing = false
	switch {
	case m.err != nil:
		return tea.Batch(core.Fail(fmt.Errorf("%s: %w", m.name, m.err)), core.Send(core.RunChecks{}))
	case m.reopen:
		return core.Send(core.Reopen{})
	}
	return tea.Batch(core.Say(m.name+" is fixed"), core.Send(core.RunChecks{}))
}

func report(st *core.State, cs []core.Check) string {
	var b strings.Builder
	b.WriteString("dokky " + st.Snap.Version + " health\n\n")
	for _, c := range cs {
		mark := map[core.Tone]string{core.Good: "ok", core.Warn: "warn", core.Bad: "fail", core.Busy: "busy"}[c.Tone]
		b.WriteString(mark + "  " + c.Name + ": " + c.Status + "\n")
		for _, f := range c.Facts {
			b.WriteString("      " + f[0] + ": " + f[1] + "\n")
		}
		switch {
		case c.Auto != "":
			b.WriteString("      " + c.Auto + "\n")
		case c.Fix != "":
			b.WriteString("      Needs you: " + c.Fix + "\n")
		}
	}
	return b.String()
}

func (h *Health) Actions(st *core.State) []core.Action {
	cs := Checks(st)
	c := cs[h.selected(cs)]
	var acts []core.Action
	switch {
	case !h.split && h.open:
		acts = append(acts, core.Action{Key: "esc", Label: "back"})
		if len(cs) > 1 {
			acts = append(acts, core.Action{Key: "↑↓", Label: "next"})
		}
	case len(cs) > 1:
		acts = append(acts, core.Action{Key: "↑↓", Label: "move"})
	}
	switch {
	case !h.split && !h.open:
		acts = append(acts, core.Action{Key: "enter", Label: "details"})
	case actLabel(st, c) != "":
		acts = append(acts, core.Action{Key: "enter", Label: actLabel(st, c)})
	}
	if len(Repairs(st, false)) > 0 && !st.Fixing {
		acts = append(acts, core.Action{Key: "f", Label: "fix all", Accent: true})
	}
	acts = append(acts, core.Action{Key: "c", Label: "copy report"})
	if !st.Checking {
		acts = append(acts, core.Action{Key: "r", Label: "check again"})
	}
	return acts
}

func (h *Health) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	if st.Fixing {
		return true
	}
	for _, c := range Checks(st) {
		if c.Tone == core.Busy {
			return true
		}
	}
	return false
}

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
	cs := Checks(st)
	sel := h.selected(cs)
	h.split = w >= twoCol
	if !h.split && h.open {
		b.Lines(2, 0, detail(st, cs[sel], w-4, ht))
		return b.Layer()
	}
	bh := 1
	if ht >= shortBox+bannerH {
		bh = bannerH
	}
	h.banner(b, st, cs, w, bh)
	y := bh + 1
	room := ht - y
	if room <= 0 {
		return b.Layer()
	}
	h.page = room
	lw := w
	if h.split {
		lw = listW
	}
	start := max(0, sel-room+1)
	for i := start; i < len(cs) && i-start < room; i++ {
		b.Hit(0, y+i-start, "check:"+cs[i].Name, row(st, cs[i], lw, i == sel))
	}
	c := cs[sel]
	if h.split {
		dw := w - lw - 4
		rule := make([]string, room)
		for i := range rule {
			rule[i] = ui.Paint(ui.G.V, p.Line, nil, false)
		}
		b.Lines(lw+1, y, rule)
		b.Lines(lw+3, y, detail(st, c, dw, room))
		return b.Layer()
	}
	if n := min(len(cs), room); room-n >= 4 {
		dy := y + n + 1
		b.Put(0, dy, ui.Paint(strings.Repeat(ui.G.H, w), p.Line, nil, false))
		b.Lines(3, dy+1, detail(st, c, w-6, room-n-1))
	}
	return b.Layer()
}

// the name and status, what the check means, its facts in a fixed order, then the fix
func detail(st *core.State, c core.Check, w, limit int) []string {
	p := ui.P()
	g, s := tone(c.Tone)
	if c.Tone == core.Busy {
		g, s.Fg = ui.Spin(st.T), ui.SpinFg(st.T)
	}
	out := []string{ui.L(nil).B(c.Name, p.Text).Right(w, ui.L(nil).B(g, s.Fg).Sp(1).T(c.Status, s.WordsFg)), ""}
	out = append(out, wrap(c.Detail, w, p.Muted)...)
	kw := 11
	for _, f := range c.Facts {
		kw = max(kw, ui.Width(f[0])+2)
	}
	kw = min(kw, w/2)
	if len(c.Facts) > 0 {
		out = append(out, "")
	}
	for _, f := range c.Facts {
		v, vc := f[1], p.Text
		if v == "" {
			v, vc = ui.G.Dash, p.Faint
		}
		lines := strings.Split(ansi.Wrap(core.Safe(v), w-kw, ""), "\n")
		for i, l := range lines[:min(3, len(lines))] {
			k := ""
			if i == 0 {
				k = core.Fit(f[0], kw-2)
			}
			out = append(out, ui.L(nil).Raw(ui.L(nil).T(k, p.Faint).Pad(kw)).T(l, vc).String())
		}
	}
	switch {
	case c.Tone != core.Warn && c.Tone != core.Bad:
	case c.Repair != nil:
		out = append(out, "")
		out = append(out, wrap(c.Auto, w, p.Accent)...)
	case c.Fix != "":
		out = append(out, "", ui.Paint("Needs you", s.Fg, nil, true))
		out = append(out, wrap(c.Fix, w, p.Text)...)
	}
	return out[:min(len(out), max(0, limit))]
}

func wrap(s string, w int, fg color.Color) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		for _, l := range strings.Split(ansi.Wordwrap(core.Safe(para), w, ""), "\n") {
			out = append(out, ui.Paint(l, fg, nil, false))
		}
	}
	return out
}

func row(st *core.State, c core.Check, w int, on bool) string {
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		g, s := tone(c.Tone)
		if c.Tone == core.Busy {
			g, s.Fg = ui.Spin(st.T), ui.SpinFg(st.T)
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
func (hl *Health) banner(b *ui.Box, st *core.State, cs []core.Check, w, h int) {
	p := ui.P()
	n, bad := 0, false
	for _, c := range cs {
		switch {
		case c.Expected:
		case c.Tone == core.Bad:
			n, bad = n+1, true
		case c.Tone == core.Warn:
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
			right.B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Checking", p.Muted)
		case !st.Checked.IsZero():
			right.T("Checked "+core.Ago(st.Checked, st.Now), p.Muted)
		}
		lines[mid] = text.Right(w-1, right)
		b.Lines(0, 0, lines)
		return
	}
	verb := " need attention."
	if n == 1 {
		verb = " needs attention."
	}
	text, key, action := core.Count(n, "problem")+verb, "", ""
	switch {
	case st.Fixing:
		text = ui.Spin(st.T) + " Fixing"
	case len(Repairs(st, false)) > 0:
		key, action = "f", "fix all"
	}
	ui.PutBanner(b, 0, w, h, bad, text, key, action)
}

// the problems Fix all can repair, anything that reopens dokky last; the pass on open skips reopening twice
func Repairs(st *core.State, auto bool) []core.Check {
	var now, last []core.Check
	for _, c := range st.Checks {
		switch {
		case c.Repair == nil || c.Tone != core.Warn && c.Tone != core.Bad:
		case c.Reopen && auto && st.Deps.Reopened:
		case c.Reopen:
			last = append(last, c)
		default:
			now = append(now, c)
		}
	}
	return append(now, last...)
}
