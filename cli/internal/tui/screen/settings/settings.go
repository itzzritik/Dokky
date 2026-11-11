package settings

import (
	"context"
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/devices"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/dropdown"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type kind int

const (
	action kind = iota
	toggle
	choice // a value with a caret that opens a dropdown under the row
	info
	danger
	update
)

// what s does when the server signed this computer out
const removeLabel = "remove this device"

type row struct {
	group, label, value string
	icon                string
	note                string // painted on the group's heading line, right side
	kind                kind
	tone                color.Color
	on                  bool // a toggle's state
	apart               bool // a gap above, no heading
	run                 func(*core.State) tea.Cmd
}

type Settings struct {
	sel, top, page int
	selLabel       string // rows come and go live, so the selection follows the label
	anchor         int    // the body row the selection was last drawn on, where a dropdown opens
	pending        *api.Settings
}

func New() *Settings { return &Settings{} }

// presses faster than the daemon's echo build on the last change
func (s *Settings) current(st *core.State) api.Settings {
	if s.pending != nil && *s.pending != st.Snap.Settings {
		return *s.pending
	}
	s.pending = nil
	return st.Snap.Settings
}

func (s *Settings) set(st *core.State, change func(*api.Settings)) tea.Cmd {
	v := s.current(st)
	change(&v)
	s.pending = &v
	c := st.C
	return func() tea.Msg { return setDone{c.SetSettings(context.Background(), v)} }
}

type setDone struct{ err error }

// sync runs every 30 seconds on its own, so it has no row
func (s *Settings) rows(st *core.State) []row {
	p := ui.P()
	set, acct := s.current(st), st.Snap.Account
	rows := []row{
		s.toggle("General", "Notify when a drive is ejected", ui.G.Icon.Notify, set.NotifyDisconnect, func(v *api.Settings, on bool) { v.NotifyDisconnect = on }),
		{label: "Cached files", icon: ui.G.Icon.Cache, value: core.Size(st.Snap.CacheBytes), kind: action, tone: p.Muted, run: openStorage},
		s.toggle("", "Automatic updates", ui.G.Icon.Update, set.AutoUpdate, func(v *api.Settings, on bool) { v.AutoUpdate = on }),
	}
	if st.UpdateReady() {
		rows = append([]row{{label: "Update dokky", icon: ui.G.Icon.Update, value: st.Snap.Version + " " + ui.G.Arrow + " " + st.Latest, kind: update, tone: p.Accent,
			run: func(*core.State) tea.Cmd { return core.Send(core.OfferUpdate{}) }}}, rows...)
	}
	rows = append(rows, s.security(st)...)
	if acct.LoggedIn {
		rows = append(rows,
			row{group: "Account", label: "Your devices", icon: ui.G.Icon.Devices, kind: action, run: devices.Open},
			row{label: "Remove this device", icon: ui.G.Icon.SignOut, kind: action, run: func(*core.State) tea.Cmd { return devices.RemoveSelf() }})
	} else {
		rows = append(rows, row{group: "Account", label: "Sign in", icon: ui.G.Icon.SignIn, kind: action, run: gate.Open})
	}
	return append(rows, row{apart: true, label: "Uninstall dokky", icon: ui.G.Icon.Remove, kind: danger, run: s.uninstall})
}

// an on/off setting, shown as On or Off as Forged has it
func (s *Settings) toggle(group, label, icon string, on bool, set func(*api.Settings, bool)) row {
	return row{group: group, label: label, icon: icon, kind: toggle, on: on, run: func(st *core.State) tea.Cmd {
		return s.set(st, func(v *api.Settings) { set(v, !on) })
	}}
}

// a setting with a few values: the row shows the current one and opens a dropdown under itself
func (s *Settings) choice(label, icon string, opts []string, cur int, choose func(*core.State, int) tea.Cmd) row {
	value := ""
	if cur >= 0 {
		value = opts[cur]
	}
	return row{label: label, icon: icon, value: value, kind: choice, run: func(st *core.State) tea.Cmd {
		return dropdown.Open(&dropdown.Dropdown{Items: opts, Cur: cur, Anchor: s.anchor,
			Choose: func(i int) tea.Cmd { return choose(st, i) }})
	}}
}

func (s *Settings) uninstall(st *core.State) tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Uninstall dokky?",
		Line:     "dokky cleans up after itself in about 10 minutes.",
		Do:       "Uninstall",
		Run: func(st *core.State) tea.Cmd {
			run := st.Deps.Uninstall
			return func() tea.Msg {
				if run == nil {
					return nil
				}
				if err := run(); err != nil {
					return core.Toast{Text: core.Report(fmt.Errorf("could not uninstall: %w", err)), Bad: true}
				}
				return core.Toast{Text: "dokky is uninstalled. It cleans up in about 10 minutes."}
			}
		},
	})
}

// the selected row, found by its label; when it has gone, the one now in its place
func (s *Settings) selected(rows []row) row {
	for i, r := range rows {
		if r.label == s.selLabel {
			s.sel = i
		}
	}
	s.sel = max(0, min(s.sel, len(rows)-1))
	s.selLabel = rows[s.sel].label
	return rows[s.sel]
}

func (s *Settings) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	rows := s.rows(st)
	r := s.selected(rows)
	switch m := msg.(type) {
	case setDone:
		if m.err != nil {
			s.pending = nil
			return s, core.Fail(m.err)
		}
	case core.Click:
		for i, x := range rows {
			if m.ID == "row:"+x.label {
				s.sel, s.selLabel = i, x.label
				if x.run != nil {
					return s, x.run(st)
				}
			}
		}
	case tea.KeyPressMsg:
		k := m.String()
		if i, ok := ui.Move(k, s.sel, len(rows), s.page); ok {
			s.sel, s.selLabel = i, rows[i].label
			return s, nil
		}
		switch k {
		case "enter":
			if r.run != nil {
				return s, r.run(st)
			}
		case "space":
			// space flips switches and nothing else, so it can't start an uninstall
			if r.kind == toggle {
				return s, r.run(st)
			}
		case "s":
			if st.Snap.Account.SignedOut {
				return s, devices.RemoveSelf()
			}
		}
	}
	return s, nil
}

func (s *Settings) Actions(st *core.State) []core.Action {
	rows := s.rows(st)
	r := s.selected(rows)
	var acts []core.Action
	if len(rows) > 1 {
		acts = append(acts, core.Action{Key: "↑↓", Label: "move"})
	}
	switch r.kind {
	case toggle:
		label := "turn on"
		if r.on {
			label = "turn off"
		}
		acts = append(acts, core.Action{Key: "space", Label: label})
	case choice:
		acts = append(acts, core.Action{Key: "enter", Label: "change"})
	case action:
		acts = append(acts, core.Action{Key: "enter", Label: "run"})
	case danger:
		acts = append(acts, core.Action{Key: "enter", Label: "uninstall", Danger: true})
	case update:
		acts = append(acts, core.Action{Key: "enter", Label: "update", Accent: true})
	}
	if st.Snap.Account.SignedOut {
		acts = append(acts, core.Action{Key: "s", Label: removeLabel})
	}
	return acts
}

func (s *Settings) Animating(*core.State) bool { return false }

func initials(name, email string) string {
	var out []rune
	for _, f := range strings.Fields(name) {
		out = append(out, []rune(strings.ToUpper(f))[0])
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 && email != "" {
		out = []rune(strings.ToUpper(email[:1]))
	}
	return string(out)
}

func (s *Settings) View(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	cw := w
	acct := st.Snap.Account
	y := 0
	if acct.LoggedIn && (acct.SignedOut || acct.Offline) {
		bh := 1
		if h >= 21 {
			bh = 3
		}
		text, key, action := "Can't reach dokky.ritik.me", "", ""
		if acct.SignedOut {
			text, key, action = "Signed out on the server", "s", removeLabel
		}
		ui.PutBanner(b, 0, w, bh, false, text, key, action)
		y = bh + 1
	}
	if acct.LoggedIn {
		in := core.Safe(initials(acct.Name, acct.Email))
		blank := ui.Paint("      ", nil, p.Accent, false)
		b.Lines(2, y, []string{blank, ui.Paint("  "+in+strings.Repeat(" ", max(0, 4-ui.Width(in))), p.OnAccent, p.Accent, true), blank})
		b.Put(10, y, ui.Paint(core.Safe(cmpOr(acct.Name, acct.Email)), p.Text, nil, true))
		b.Put(10, y+1, ui.Paint(core.Safe(acct.Email), p.Muted, nil, false))
		mark, mc := ui.G.Check, p.Kelp
		if acct.SignedOut || acct.Offline {
			mark, mc = ui.G.Dash, p.Faint
		}
		b.Put(10, y+2, ui.L(nil).B(mark, mc).Sp(1).T("Synced "+core.Ago(acct.LastSyncAt, st.Now), p.Faint).String())
	} else {
		b.Put(2, y, ui.Paint("This computer only", p.Text, nil, true))
		b.Put(2, y+1, ui.Paint("Your drives stay on this computer.", p.Muted, nil, false))
	}
	y += 4
	rows := s.rows(st)
	s.selected(rows)
	s.page = max(1, h-y-2)
	var lines []ui.ListLine
	for i, r := range rows {
		if (r.group != "" || r.apart) && len(lines) > 0 {
			lines = append(lines, ui.ListLine{Item: -1})
		}
		if r.group != "" {
			head := ui.L(nil).Sp(2).T(r.group, p.Faint)
			if r.note != "" {
				lines = append(lines, ui.ListLine{Text: head.Right(cw-1, ui.L(nil).Raw(r.note)), Item: -1})
			} else {
				lines = append(lines, ui.ListLine{Text: head.String(), Item: -1})
			}
		}
		lines = append(lines, ui.ListLine{Text: s.line(r, i == s.sel, cw), Item: i, ID: "row:" + r.label})
	}
	ui.List(b, y, h-y, &s.top, s.sel, lines)
	for i, l := range lines {
		if l.Item == s.sel {
			s.anchor = y + i - s.top
		}
	}
	return b.Layer()
}

// a settings row as Forged draws it: an icon and the label left, the value right
func (s *Settings) line(r row, on bool, w int) string {
	p := ui.P()
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		lc, ic := p.Text, p.Muted
		switch {
		case r.kind == danger:
			lc, ic = p.Coral, p.Coral
		case r.kind == update:
			lc, ic = p.Accent, p.Accent
		case on:
			ic = p.Accent
		}
		l := ui.L(bg)
		if r.icon != "" {
			l.T(r.icon, ic).Sp(2)
		}
		if on || r.kind == update {
			l.B(r.label, lc)
		} else {
			l.T(r.label, lc)
		}
		right := ui.L(bg)
		switch r.kind {
		case toggle:
			if r.on {
				right.T(ui.G.Here+" On", p.Accent)
			} else {
				right.T(ui.G.Ring+" Off", p.Muted)
			}
		case choice:
			right.T(r.value+" "+ui.G.Caret, p.Text)
		default:
			right.T(r.value, cmpColor(r.tone, p.Muted))
		}
		return l.Right(iw-1, right)
	})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cmpColor(a, b color.Color) color.Color {
	if a != nil {
		return a
	}
	return b
}
