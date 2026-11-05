package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type latest struct{ version string }

type updated struct {
	version string
	ok      bool
}

func (a *App) checkUpdate() tea.Cmd {
	if a.updateChecked || !a.st.Connected || a.st.Snap.Settings.AutoUpdate {
		return nil
	}
	a.updateChecked = true
	c := a.st.C
	return func() tea.Msg {
		v, _ := c.UpdateCheck(context.Background())
		return latest{v}
	}
}

func (a *App) updateReady() bool { return a.st.Latest != "" && !a.st.Snap.Settings.AutoUpdate }

// once a session, under any open window
func (a *App) onLatest(m latest) {
	a.st.Latest = m.version
	if a.updateReady() && a.gate == nil {
		a.windows = append([]core.Screen{&update{btn: 1}}, a.windows...)
	}
}

type update struct {
	btn  int // 0 later, 1 update
	req  core.Req
	busy bool
	err  string
}

func (m *update) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m, core.Close(m)
		case "left", "right":
			m.btn = 1 - m.btn
		case "enter":
			return m, m.press(st)
		}
	case core.Click:
		switch msg.ID {
		case "btn:later":
			m.btn = 0
			return m, m.press(st)
		case "btn:update":
			m.btn = 1
			return m, m.press(st)
		}
	case core.Reply:
		if msg.Req != m.req || !m.busy {
			return m, nil
		}
		m.busy = false
		if msg.Err != nil {
			m.err = core.Report(msg.Err)
			return m, nil
		}
		u, _ := msg.Val.(updated)
		st.Latest = ""
		if !u.ok {
			return m, tea.Batch(core.Close(m), core.Say("dokky is up to date"))
		}
		return m, tea.Batch(core.Close(m), core.Say("Updated to "+u.version+". Reopen dokky to use it."))
	}
	return m, nil
}

func (m *update) press(st *core.State) tea.Cmd {
	if m.btn == 0 {
		return core.Close(m)
	}
	if m.busy {
		return nil
	}
	m.busy, m.err, m.req = true, "", core.NextReq()
	return core.Do(st, m.req, func(ctx context.Context, c *client.Client) (any, error) {
		v, ok, err := c.Update(ctx)
		return updated{v, ok}, err
	})
}

func (m *update) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(56, maxW), Title: "Update available", Sub: m.err, SubBad: m.err != "", Active: m.btn}
	uploads := 0
	for _, d := range st.Snap.Drives {
		uploads += d.Uploads
	}
	body := ui.NewBox()
	body.Put(0, 0, ui.L(p.Surface).T(st.Snap.Version, p.Text).Sp(1).T(ui.G.Arrow, p.Muted).Sp(1).B(st.Latest, p.Accent).Pad(win.Inner()))
	note, nc := "dokky restarts in the background.", p.Muted
	if uploads > 0 {
		note, nc = core.Count(uploads, "upload")+" not finished. dokky restarts to update.", p.Buoy
	}
	body.Put(0, 2, win.Text(note, nc))
	rows := 3
	if m.busy {
		body.Put(0, 4, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Updating", p.Muted).Pad(win.Inner()))
		rows = 5
	} else {
		win.Buttons = []ui.Btn{{ID: "later", Label: "Later"}, {ID: "update", Label: "Update", Primary: true}}
	}
	win.Body = body
	win.Fit(rows, maxH)
	return win.Layer()
}

func (m *update) Actions(*core.State) []core.Action {
	if m.busy {
		return []core.Action{{Key: "esc", Label: "close"}}
	}
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "later"}}
}

func (m *update) Animating(*core.State) bool { return m.busy && !ui.Reduced() }
