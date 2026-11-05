package settings

import (
	"context"
	"image/color"
	"os"
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

type checked struct{ problems []string }

type Settings struct {
	sel, top int
	doctor   []string
	checked  bool
	checking bool
	syncing  bool
	pending  *api.Settings
}

func New() *Settings { return &Settings{} }

var themes = []string{"auto", "dark", "light"}

func themeName(t string) string {
	switch t {
	case "dark":
		return "Dark"
	case "light":
		return "Light"
	}
	return "Follow my terminal"
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

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
	updates := "Install automatically"
	if !set.AutoUpdate {
		updates = "Off"
	}
	motion := "Full"
	if set.Motion == "reduced" {
		motion = "Reduced"
	}
	doctor, tone := "", p.Text
	switch {
	case s.checking:
		doctor = ui.Spin(st.T) + " Checking"
	case s.checked && len(s.doctor) == 0:
		doctor, tone = ui.G.Check+" All good", p.Kelp
	case s.checked:
		doctor, tone = strconv.Itoa(len(s.doctor))+" to fix", p.Coral
	}
	rows = append(rows,
		row{group: "General", label: "Tell me when a drive disconnects", value: onOff(set.NotifyDisconnect), kind: choice, run: func(st *core.State) tea.Cmd {
			return s.set(st, func(v *api.Settings) { v.NotifyDisconnect = !v.NotifyDisconnect })
		}},
		row{label: "Updates", value: updates, kind: choice, run: func(st *core.State) tea.Cmd {
			return s.set(st, func(v *api.Settings) { v.AutoUpdate = !v.AutoUpdate })
		}},
		row{group: "Look", label: "Theme", value: themeName(set.Theme), kind: choice, run: func(st *core.State) tea.Cmd {
			return s.set(st, func(v *api.Settings) {
				i := 0
				for k, t := range themes {
					if t == v.Theme {
						i = k
					}
				}
				v.Theme = themes[(i+1)%len(themes)]
			})
		}},
		row{label: "Motion", value: motion, kind: choice, run: func(st *core.State) tea.Cmd {
			return s.set(st, func(v *api.Settings) {
				v.Motion = "reduced"
				if motion == "Reduced" {
					v.Motion = "full"
				}
			})
		}},
		row{group: "Storage", label: "Cached files", value: core.Size(st.Snap.CacheBytes), kind: info},
		row{label: "Cached files folder", value: home(st.Snap.CacheDir), kind: info, tone: p.Faint},
		row{group: "Help", label: "Check this computer", value: doctor, kind: action, tone: tone, run: s.check},
		row{label: "Version", value: st.Snap.Version, kind: info},
	)
	if acct.LoggedIn {
		rows = append(rows, row{group: "Account", label: "Sign out", kind: action, run: func(*core.State) tea.Cmd { return devices.SignOut() }})
	} else {
		rows = append(rows, row{group: "Account", label: "Sign in", kind: action, run: gate.Open})
	}
	return append(rows, row{label: "Uninstall dokky", kind: danger, run: s.uninstall})
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

func (s *Settings) check(st *core.State) tea.Cmd {
	if s.checking || st.Deps.Doctor == nil {
		return nil
	}
	s.checking = true
	doctor := st.Deps.Doctor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return checked{doctor(ctx)}
	}
}

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
					return core.Toast{Text: "Could not uninstall: " + core.Safe(err.Error()), Bad: true}
				}
				return core.Toast{Text: "dokky is uninstalled. It cleans up in about 10 minutes."}
			}
		},
	})
}

func (s *Settings) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	rows := s.rows(st)
	switch m := msg.(type) {
	case checked:
		s.checking, s.checked, s.doctor = false, true, m.problems
		if len(m.problems) > 0 {
			return s, core.Send(core.Toast{Text: m.problems[0] + ". Run dokky doctor for the fix.", Bad: true})
		}
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
	return acts
}

func (s *Settings) Animating(*core.State) bool { return (s.checking || s.syncing) && !ui.Reduced() }

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
	acct := st.Snap.Account
	if acct.LoggedIn {
		in := core.Safe(initials(acct.Name, acct.Email))
		blank := ui.Paint("      ", nil, p.Accent, false)
		tile := []string{blank, ui.Paint("  "+in+strings.Repeat(" ", max(0, 4-ui.Width(in))), p.OnAccent, p.Accent, true), blank}
		b.Lines(2, 0, tile)
		b.Put(10, 0, ui.Paint(core.Safe(cmpOr(acct.Name, acct.Email)), p.Text, nil, true))
		b.Put(10, 1, ui.Paint(core.Safe(acct.Email), p.Muted, nil, false))
		b.Put(10, 2, ui.Paint("Synced "+core.Ago(acct.LastSyncAt, st.Now), p.Muted, nil, false))
	} else {
		b.Put(2, 0, ui.Paint("This computer only", p.Text, nil, true))
		b.Put(2, 1, ui.Paint("Your drives stay on this computer.", p.Muted, nil, false))
	}
	rows := s.rows(st)
	s.sel = max(0, min(s.sel, len(rows)-1))
	type line struct {
		text string
		idx  int
	}
	var lines []line
	for i, r := range rows {
		if r.group != "" {
			if len(lines) > 0 {
				lines = append(lines, line{"", -1})
			}
			lines = append(lines, line{ui.L(nil).Sp(2).T(r.group, p.Muted).String(), -1})
		}
		lines = append(lines, line{s.line(r, i == s.sel, w), i})
	}
	top := 4
	room := h - top
	at := 0
	for i, l := range lines {
		if l.idx == s.sel {
			at = i
		}
	}
	if at < s.top {
		s.top = max(0, at-1)
	}
	if at >= s.top+room {
		s.top = at - room + 1
	}
	for i := s.top; i < len(lines) && i < s.top+room; i++ {
		if lines[i].idx >= 0 {
			b.Hit(0, top+i-s.top, "row:"+strconv.Itoa(lines[i].idx), lines[i].text)
		} else {
			b.Put(0, top+i-s.top, lines[i].text)
		}
	}
	return b.Layer()
}

func (s *Settings) line(r row, on bool, w int) string {
	p := ui.P()
	return ui.RowLine(w, on, nil, func(bg color.Color, iw int) string {
		lc := ui.Mix(p.Muted, p.Text, .7)
		if on {
			lc = p.Text
		}
		if r.kind == danger {
			lc = p.Coral
		}
		l := ui.L(bg)
		if on {
			l.B(r.label, lc)
		} else {
			l.T(r.label, lc)
		}
		tone := r.tone
		if tone == nil {
			tone = p.Text
		}
		right := ui.L(bg)
		switch r.kind {
		case choice:
			right.T(r.value, tone).Sp(2).T(ui.G.Caret, p.Muted)
		case action, danger:
			if r.value != "" {
				right.T(r.value, tone).Sp(2)
			}
			right.T(ui.G.Arrow, p.Muted)
		default:
			right.T(r.value, cmpColor(r.tone, p.Faint))
		}
		return l.Right(iw, right)
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
