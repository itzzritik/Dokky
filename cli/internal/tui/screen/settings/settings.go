package settings

import (
	"context"
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

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
	toggle
	info
	danger
	update
)

const slide = 180 * time.Millisecond

type row struct {
	group, label, value string
	note                string // painted on the group's heading line, right side
	kind                kind
	tone                color.Color
	frac                float64 // a switch's knob, 0 off to 1 on
	apart               bool    // a gap above, no heading
	run                 func(*core.State) tea.Cmd
}

type Settings struct {
	sel, top int
	pending  *api.Settings
	flipped  map[string]time.Time
}

func New() *Settings { return &Settings{flipped: map[string]time.Time{}} }

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
		s.toggle(st, "General", "Notify when a drive is ejected", set.NotifyDisconnect, func(v *api.Settings, on bool) { v.NotifyDisconnect = on }),
		// between the switches, so their pills don't touch
		{label: "Cached files", value: core.Size(st.Snap.CacheBytes), kind: action, tone: p.Muted, run: openStorage},
		s.toggle(st, "", "Automatic updates", set.AutoUpdate, func(v *api.Settings, on bool) { v.AutoUpdate = on }),
	}
	if st.UpdateReady() {
		rows = append([]row{{label: "Update dokky", value: st.Snap.Version + " " + ui.G.Arrow + " " + st.Latest, kind: update, tone: p.Accent,
			run: func(*core.State) tea.Cmd { return core.Send(core.OfferUpdate{}) }}}, rows...)
	}
	rows = append(rows, s.security(st)...)
	if acct.LoggedIn {
		rows = append(rows,
			row{group: "Account", label: "Your devices", kind: action, run: devices.Open},
			row{label: "Remove this device", kind: action, run: func(*core.State) tea.Cmd { return devices.RemoveSelf() }})
	} else {
		rows = append(rows, row{group: "Account", label: "Sign in", kind: action, run: gate.Open})
	}
	return append(rows, row{apart: true, label: "Uninstall dokky", kind: danger, run: s.uninstall})
}

// an on/off setting as a switch that slides when flipped
func (s *Settings) toggle(st *core.State, group, label string, on bool, set func(*api.Settings, bool)) row {
	frac := 0.0
	if on {
		frac = 1
	}
	if at, ok := s.flipped[label]; ok {
		frac = ui.Ease(1-frac, frac, at, st.Now, slide)
	}
	return row{group: group, label: label, kind: toggle, frac: frac, run: func(st *core.State) tea.Cmd {
		s.flipped[label] = st.Now
		return s.set(st, func(v *api.Settings) { set(v, !on) })
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

func (s *Settings) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	rows := s.rows(st)
	switch m := msg.(type) {
	case setDone:
		if m.err != nil {
			s.pending = nil
			return s, core.Fail(m.err)
		}
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
				return s, devices.RemoveSelf()
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
	case toggle:
		label := "turn on"
		if r.frac >= .5 {
			label = "turn off"
		}
		acts = append(acts, core.Action{Key: "enter", Label: label})
	case action:
		acts = append(acts, core.Action{Key: "enter", Label: "run"})
	case danger:
		acts = append(acts, core.Action{Key: "enter", Label: "uninstall", Danger: true})
	case update:
		acts = append(acts, core.Action{Key: "enter", Label: "update", Accent: true})
	}
	if st.Snap.Account.SignedOut {
		acts = append(acts, core.Action{Key: "s", Label: "remove this device"})
	}
	if st.Snap.Vault.State == api.VaultUnlocked {
		acts = append(acts, core.Action{Key: "ctrl+l", Label: "lock"})
	}
	return acts
}

func (s *Settings) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	for _, at := range s.flipped {
		if ui.Easing(at, st.Now, slide) {
			return true
		}
	}
	return false
}

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
		lines = append(lines, ui.ListLine{Text: s.line(r, i == s.sel, cw), Item: i, ID: "row:" + strconv.Itoa(i)})
	}
	ui.List(b, y, h-y, &s.top, s.sel, lines)
	return b.Layer()
}

// a settings row: label left, value and its glyph right
func (s *Settings) line(r row, on bool, w int) string {
	p := ui.P()
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		lc := p.Text
		switch r.kind {
		case danger:
			lc = p.Coral
		case update:
			lc = p.Accent
		}
		l := ui.L(bg)
		if on || r.kind == update {
			l.B(r.label, lc)
		} else {
			l.T(r.label, lc)
		}
		right := ui.L(bg)
		switch r.kind {
		case toggle:
			right.Raw(ui.Switch(r.frac, bg))
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
