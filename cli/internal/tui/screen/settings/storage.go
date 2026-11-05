package settings

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// what the cache holds, where it lives, and a way to empty it
type storage struct{ btn int }

func openStorage(*core.State) tea.Cmd { return core.Open(&storage{btn: 1}) }

func (s *storage) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return s, core.Close(s)
		case "left", "right":
			s.btn = 1 - s.btn
		case "enter":
			return s, s.press(st)
		}
	case core.Click:
		switch m.ID {
		case "btn:clear":
			s.btn = 0
			return s, s.press(st)
		case "btn:close":
			s.btn = 1
			return s, s.press(st)
		}
	}
	return s, nil
}

func (s *storage) press(st *core.State) tea.Cmd {
	if s.btn == 1 {
		return core.Close(s)
	}
	return confirm.Open(&confirm.Confirm{
		Question: "Clear cached files?",
		Line:     "Connected drives reconnect for a moment.",
		Do:       "Clear",
		Run: func(st *core.State, _ bool) tea.Cmd {
			c := st.C
			return tea.Batch(core.Say("Clearing cached files"), func() tea.Msg {
				if err := c.ClearCache(context.Background()); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return core.Toast{Text: "Cached files cleared"}
			})
		},
	})
}

func (s *storage) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(72, maxW), Title: "Cached files", Sub: core.Size(st.Snap.CacheBytes) + " on this computer, so files open fast.", Active: s.btn,
		Buttons: []ui.Btn{{ID: "clear", Label: "Clear cache", Danger: true}, {ID: "close", Label: "Close", Primary: true}}}
	cw := win.Inner()
	body := ui.NewBox()
	y := 0
	for _, d := range st.Snap.Drives {
		if d.CacheBytes <= 0 || y >= win.Room(maxH)-2 {
			continue
		}
		body.Put(0, y, ui.L(p.Surface).T(d.Name, p.Text).Right(cw, ui.L(p.Surface).T(core.Size(d.CacheBytes), p.Muted)))
		y++
	}
	if y == 0 {
		body.Put(0, 0, win.Text("Nothing is cached right now.", p.Faint))
		y = 1
	}
	body.Put(0, y+1, ui.L(p.Surface).T("Location", p.Faint).Right(cw, ui.L(p.Surface).T(core.Home(st.Snap.CacheDir), p.Muted)))
	win.Fit(y+2, maxH)
	win.Body = body
	return win.Layer()
}

func (s *storage) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "close"}}
}

func (s *storage) Animating(*core.State) bool { return false }
