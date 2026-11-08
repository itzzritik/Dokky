package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	longNotice = 60
	maxPending = 5
)

type toastGone struct{ at time.Time }

func long(t core.Toast) bool { return ui.Width(t.Text) > longNotice }

// notices wait while the gate is all there is on screen
func (a *App) waiting() bool { return a.st.Connected && a.gate != nil && len(a.windows) == 0 }

func (a *App) post(t core.Toast) tea.Cmd {
	t.Text = strings.TrimSpace(t.Text)
	hold := a.waiting() && !t.Now
	switch {
	case t.Text == "":
		return nil
	case a.toast != nil && a.toast.Text == t.Text || len(a.pending) > 0 && a.pending[len(a.pending)-1] == t:
		return nil
	case !hold && long(t):
		a.openInfo(t)
		return nil
	case hold || a.toast != nil || len(a.pending) > 0:
		a.pending = append(a.pending, t)
		a.pending = a.pending[max(0, len(a.pending)-maxPending):]
		return nil
	}
	return a.show(t)
}

func (a *App) show(t core.Toast) tea.Cmd {
	a.toast = &ui.Toast{Text: t.Text, Bad: t.Bad, Now: t.Now, At: a.st.Now}
	at := a.st.Now
	return tea.Tick(a.toast.For(), func(time.Time) tea.Msg { return toastGone{at} })
}

func (a *App) nextToast() tea.Cmd {
	for a.toast == nil && len(a.pending) > 0 && !a.waiting() {
		t := a.pending[0]
		a.pending = a.pending[1:]
		if long(t) {
			a.openInfo(t)
			continue
		}
		return a.show(t)
	}
	return nil
}

func (a *App) dropToast() tea.Cmd {
	a.toast = nil
	return a.nextToast()
}

// goes under open windows so it never interrupts one in use
func (a *App) openInfo(t core.Toast) {
	for _, w := range a.windows {
		if i, ok := w.(*info); ok && i.text == t.Text {
			return
		}
	}
	title := "dokky"
	if t.Bad {
		title = "Something went wrong"
	}
	a.windows = append([]core.Screen{&info{title: title, text: t.Text, bad: t.Bad}}, a.windows...)
}

type info struct {
	title, text string
	bad         bool
}

func (m *info) Update(msg tea.Msg, _ *core.State) (core.Screen, tea.Cmd) {
	switch m2 := msg.(type) {
	case tea.KeyPressMsg:
		if s := m2.String(); s == "enter" || s == "esc" {
			return m, core.Close(m)
		}
	case core.Click:
		if m2.ID == "btn:ok" {
			return m, core.Close(m)
		}
	}
	return m, nil
}

func (m *info) View(_ *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(60, maxW), Title: m.title, Bad: m.bad, Buttons: []ui.Btn{{ID: "ok", Label: "OK", Primary: true}}}
	lines := strings.Split(ansi.Wordwrap(m.text, win.Inner(), ""), "\n")
	lines = lines[:min(len(lines), win.Room(maxH))]
	body := ui.NewBox()
	for i, l := range lines {
		body.Put(0, i, win.Text(l, p.Text))
	}
	win.Body = body
	win.Fit(len(lines), maxH)
	return win.Layer()
}

func (m *info) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "enter", Label: "ok"}, {Key: "esc", Label: "close"}}
}

func (m *info) Animating(*core.State) bool { return false }
