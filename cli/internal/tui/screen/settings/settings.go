package settings

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/devices"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type kind int

const (
	action kind = iota
	choice
	info
	danger
)

type row struct {
	group, label, value string
	kind                kind
	tone                color.Color
	run                 func(*core.State) tea.Cmd
}

type Settings struct {
	sel, top int
	ys       []int // each row's body line in the last view
	syncing  bool
	pending  *api.Settings
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

func home(path string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, h) {
		return "~" + strings.TrimPrefix(path, h)
	}
	return path
}

func (s *Settings) rows(st *core.State) []row {
	p := ui.P()
	set, acct := s.current(st), st.Snap.Account
	var rows []row
	if acct.LoggedIn {
		label := "Sync now"
		if s.syncing {
			label = "Syncing"
		}
		rows = append(rows, row{group: "Sync", label: label, kind: action, run: s.sync})
	}
	rows = append(rows,
		s.choice("General", "Notify when a drive disconnects", []string{"On", "Off"}, set.NotifyDisconnect, func(v *api.Settings, on bool) { v.NotifyDisconnect = on }),
		s.choice("", "Updates", []string{"Install automatically", "Off"}, set.AutoUpdate, func(v *api.Settings, on bool) { v.AutoUpdate = on }),
		row{group: "Storage", label: "Cached files", value: core.Size(st.Snap.CacheBytes), kind: action, tone: p.Muted, run: openStorage},
	)
	if acct.LoggedIn {
		rows = append(rows,
			row{group: "Account", label: "Devices", value: "Your computers", kind: action, tone: p.Faint, run: devices.Open},
			row{label: "Sign out", kind: action, run: func(*core.State) tea.Cmd { return devices.SignOut() }})
	} else {
		rows = append(rows, row{group: "Account", label: "Sign in", kind: action, run: gate.Open})
	}
	return append(rows, row{label: "Uninstall dokky", kind: danger, run: s.uninstall})
}

// a two-way setting; its first option turns it on
func (s *Settings) choice(group, label string, opts []string, on bool, set func(*api.Settings, bool)) row {
	cur := 1
	if on {
		cur = 0
	}
	return row{group: group, label: label, value: opts[cur], kind: choice, run: func(st *core.State) tea.Cmd {
		anchor := 0
		if s.sel < len(s.ys) {
			anchor = s.ys[s.sel]
		}
		return core.Open(&dropdown{title: label, items: opts, cur: cur, cursor: cur, anchor: anchor, choose: func(st *core.State, i int) tea.Cmd {
			return s.set(st, func(v *api.Settings) { set(v, i == 0) })
		}})
	}}
}

func (s *Settings) sync(st *core.State) tea.Cmd {
	if s.syncing {
		return nil
	}
	s.syncing = true
	c := st.C
	return func() tea.Msg { return synced{c.SyncNow(context.Background())} }
}

type synced struct{ err error }

func (s *Settings) uninstall(st *core.State) tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Uninstall dokky?",
		Line:     "dokky cleans up after itself in about 10 minutes.",
		Do:       "Uninstall",
		Run: func(st *core.State, _ bool) tea.Cmd {
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

func (s *Settings) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	rows := s.rows(st)
	switch m := msg.(type) {
	case setDone:
		if m.err != nil {
			s.pending = nil
			return s, core.Fail(m.err)
		}
	case synced:
		s.syncing = false
		if m.err != nil {
			return s, core.Fail(m.err)
		}
		return s, core.Say("Synced")
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "row:")); err == nil && i < len(rows) {
			s.sel = i
			if r := rows[i]; r.run != nil {
				return s, r.run(st)
			}
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "up":
			s.sel = max(0, s.sel-1)
		case "down":
			s.sel = min(len(rows)-1, s.sel+1)
		case "enter", "space":
			if r := rows[min(s.sel, len(rows)-1)]; r.run != nil {
				return s, r.run(st)
			}
		case "s":
			if st.Snap.Account.SignedOut {
				return s, devices.SignOut()
			}
		}
	}
	return s, nil
}

func (s *Settings) Actions(st *core.State) []core.Action {
	rows := s.rows(st)
	r := rows[min(s.sel, len(rows)-1)]
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	switch r.kind {
	case choice:
		acts = append(acts, core.Action{Key: "enter", Label: "change"})
	case action:
		acts = append(acts, core.Action{Key: "enter", Label: "run"})
	case danger:
		acts = append(acts, core.Action{Key: "enter", Label: "uninstall", Danger: true})
	}
	if st.Snap.Account.SignedOut {
		acts = append(acts, core.Action{Key: "s", Label: "sign out"})
	}
	return acts
}

func (s *Settings) Animating(*core.State) bool { return s.syncing && !ui.Reduced() }

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
			text, key, action = "Signed out on the server", "s", "sign out"
		}
		b.Lines(0, 0, ui.Banner(w, bh, false, text, key, action))
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
	s.sel = max(0, min(s.sel, len(rows)-1))
	var lines []ui.ListLine
	s.ys = make([]int, len(rows))
	for i, r := range rows {
		if r.group != "" {
			if len(lines) > 0 {
				lines = append(lines, ui.ListLine{Item: -1})
			}
			lines = append(lines, ui.ListLine{Text: ui.L(nil).Sp(2).T(r.group, p.Faint).String(), Item: -1})
		}
		s.ys[i] = len(lines)
		lines = append(lines, ui.ListLine{Text: s.line(r, i == s.sel, cw), Item: i, ID: "row:" + strconv.Itoa(i)})
	}
	ui.List(b, y, h-y, &s.top, s.sel, lines)
	for i := range s.ys {
		s.ys[i] += y - s.top
	}
	return b.Layer()
}

// a settings row: label left, value and its glyph right
func (s *Settings) line(r row, on bool, w int) string {
	p := ui.P()
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		lc := p.Text
		if r.kind == danger {
			lc = p.Coral
		}
		l := ui.L(bg)
		if on {
			l.B(r.label, lc)
		} else {
			l.T(r.label, lc)
		}
		right := ui.L(bg)
		switch r.kind {
		case choice:
			right.T(r.value, p.Accent).Sp(2).T(ui.G.Caret, p.Muted)
		case action, danger:
			if r.value != "" {
				right.T(r.value, cmpColor(r.tone, p.Muted)).Sp(2)
			}
			right.T(ui.G.Arrow, p.Faint)
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
