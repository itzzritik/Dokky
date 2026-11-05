package health

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"path"
	"slices"
	"strconv"
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
	sel    int
	moved  bool
	fixing bool
}

type fixed struct {
	n      int
	err    error
	reopen bool
}

func New() *Health { return &Health{} }

// live checks from the daemon's state, then the computer's own
func checks(st *core.State) []core.Check {
	out := []core.Check{drives(st), uploads(st), sync(st)}
	if len(st.Checks) == 0 && st.Checking {
		return append(out, core.Check{Name: "This computer", Status: "Checking", Detail: "Looking at the background service, updates, the cache and the keychain.", Tone: core.Busy})
	}
	return append(out, st.Checks...)
}

func drives(st *core.State) core.Check {
	c := core.Check{Name: "Drives", Detail: "Each drive and whether it is open in " + core.FileManager() + ".", Tone: core.Good}
	if len(st.Snap.Drives) == 0 {
		c.Status, c.Detail = "No drives yet", "Add a drive from Overview."
		return c
	}
	on := 0
	var failed, stale []string
	var broken []api.Drive
	for _, d := range st.Snap.Drives {
		words := ui.DriveStatus(d, st.T).Words
		switch d.State {
		case api.Connected:
			on++
		case api.Failed:
			failed, broken = append(failed, d.Name), append(broken, d)
			if d.Error != "" {
				words = d.Error
			}
		case api.Reconnect:
			stale, broken = append(stale, d.Name), append(broken, d)
		}
		c.Facts = append(c.Facts, [2]string{d.Name, words})
	}
	c.Status = strconv.Itoa(on) + " of " + strconv.Itoa(len(st.Snap.Drives)) + " connected"
	switch {
	case len(failed) > 0:
		c.Tone, c.Status = core.Bad, strings.Join(failed, ", ")+" can't connect"
	case len(stale) > 0:
		c.Tone, c.Status = core.Warn, strings.Join(stale, ", ")+" needs a reconnect"
	}
	if len(broken) > 0 {
		names := make([]string, len(broken))
		for i, d := range broken {
			names[i] = d.Name
		}
		cl := st.C
		c.Auto = "Fix all reconnects " + strings.Join(names, ", ") + "."
		c.Repair = func() error {
			var errs []error
			for _, d := range broken {
				ctx := context.Background()
				if d.State == api.Reconnect {
					if _, err := cl.Disconnect(ctx, d.ID); err != nil {
						errs = append(errs, err)
						continue
					}
				}
				if _, err := cl.Connect(ctx, d.ID); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", d.Name, err))
				}
			}
			return errors.Join(errs...)
		}
	}
	return c
}

func uploads(st *core.State) core.Check {
	c := core.Check{Name: "Uploads", Status: "All saved", Detail: "What you save to a drive stays on this computer until it reaches the cloud.", Tone: core.Good}
	moving, waiting := 0, 0
	var left int64
	for _, t := range st.Act.Now {
		if !t.Up {
			continue
		}
		left += max(0, t.Size-t.Done)
		if t.Waiting {
			waiting++
		} else {
			moving++
		}
	}
	var failed []api.Transfer
	last := ""
	for _, t := range st.Act.Earlier {
		switch {
		case !t.Up:
		case t.Error != "":
			failed = append(failed, t)
		case last == "":
			last = path.Base(t.Name) + ", " + core.Ago(t.At, st.Now)
		}
	}
	size := ""
	if left > 0 {
		size = core.Size(left)
	}
	c.Facts = [][2]string{{"Uploading", strconv.Itoa(moving)}, {"Waiting", strconv.Itoa(waiting)}, {"Left", size},
		{"Failed", strconv.Itoa(len(failed))}, {"Last saved", last}}
	switch {
	case len(failed) > 0:
		c.Tone, c.Status = core.Warn, strconv.Itoa(len(failed))+" failed"
		c.Facts = append(c.Facts, [2]string{"Error", path.Base(failed[0].Name) + ": " + failed[0].Error})
		cl := st.C
		c.Auto = "Fix all tries the failed uploads again."
		c.Repair = func() error {
			var errs []error
			for _, t := range failed {
				errs = append(errs, cl.Retry(context.Background(), t.DriveID, t.Name))
			}
			return errors.Join(errs...)
		}
	case moving+waiting > 0:
		c.Tone, c.Status = core.Busy, strconv.Itoa(moving+waiting)+" to upload"
	}
	return c
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
	if m, ok := msg.(fixed); ok {
		h.fixing = false
		switch {
		case m.reopen && m.err == nil:
			return h, core.Send(core.Reopen{})
		case m.err != nil:
			return h, tea.Batch(core.Fail(m.err), core.Send(core.RunChecks{}))
		}
		return h, tea.Batch(core.Say("Fixed "+core.Count(m.n, "problem")), core.Send(core.RunChecks{}))
	}
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
	case "f":
		return h, h.fixAll(st)
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
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	if len(repairs(st)) > 0 && !h.fixing {
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
	if h.fixing {
		return true
	}
	for _, c := range checks(st) {
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
	cs := checks(st)
	sel := h.selected(cs)
	bh := 1
	if ht >= shortBox+bannerH {
		bh = bannerH
	}
	b.Lines(0, 0, h.banner(st, cs, w, bh))
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
func (hl *Health) banner(st *core.State, cs []core.Check, w, h int) []string {
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
			right.B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Checking", p.Muted)
		case !st.Checked.IsZero():
			right.T("Checked "+core.Ago(st.Checked, st.Now), p.Muted)
		}
		lines[mid] = text.Right(w-1, right)
		return lines
	}
	verb := " need attention."
	if n == 1 {
		verb = " needs attention."
	}
	text, key, action := core.Count(n, "problem")+verb, "", ""
	switch {
	case hl.fixing:
		text = ui.Spin(st.T) + " Fixing"
	case len(repairs(st)) > 0:
		key, action = "f", "fix all"
	}
	return ui.Banner(w, h, bad, text, key, action)
}

// the problems Fix all can repair: this computer's first, then drives and uploads, anything that reopens dokky last
func repairs(st *core.State) []core.Check {
	var now, last []core.Check
	for _, c := range append(slices.Clone(st.Checks), drives(st), uploads(st)) {
		switch {
		case c.Repair == nil || c.Tone != core.Warn && c.Tone != core.Bad:
		case c.Reopen:
			last = append(last, c)
		default:
			now = append(now, c)
		}
	}
	return append(now, last...)
}

func (h *Health) fixAll(st *core.State) tea.Cmd {
	todo := repairs(st)
	if h.fixing || len(todo) == 0 {
		return nil
	}
	h.fixing = true
	return func() tea.Msg {
		var errs []error
		reopen := false
		for _, c := range todo {
			if err := c.Repair(); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", c.Name, err))
				continue
			}
			reopen = reopen || c.Reopen
		}
		return fixed{n: len(todo), err: errors.Join(errs...), reopen: reopen}
	}
}
