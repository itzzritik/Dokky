package gate

import (
	"context"
	"image/color"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type choice struct{ icon, title, sub string }

// Forged's welcome: sign in, or keep everything on this computer
func choices() []choice {
	return []choice{
		{ui.G.Icon.SignIn, "Sign in with browser", "Sync your drives across devices"},
		{ui.G.Icon.Add, "Use on this computer only", "You can sign in later from Settings"},
	}
}

type Gate struct {
	stage *ui.Stage
	sel   int
	req   core.Req
	err   string
}

func New(*core.State) *Gate { return &Gate{stage: vault.Stage()} }

func (g *Gate) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		// two choices, so every move lands on the other one
		case "up", "down", "left", "right":
			g.sel = 1 - g.sel
		case "enter", "space":
			return g, g.choose(st)
		case "esc", "q":
			return g, core.Send(core.Quit{})
		}
	case core.Click:
		switch m.ID {
		case "opt:0", "opt:1":
			g.sel = int(m.ID[4] - '0')
			return g, g.choose(st)
		case "btn:continue":
			return g, g.choose(st)
		}
	case core.Reply:
		if m.Req == g.req {
			g.err = core.Report(m.Err)
		}
	}
	return g, nil
}

func (g *Gate) choose(st *core.State) tea.Cmd {
	if g.sel == 0 {
		return Open(st)
	}
	g.req = core.NextReq()
	return Choose(st, g.req)
}

// remembers that the first launch choice was made
func Choose(st *core.State, req core.Req) tea.Cmd {
	s := st.Snap.Settings
	s.Chosen = true
	return core.Do(st, req, func(ctx context.Context, c *client.Client) (any, error) { return nil, c.SetSettings(ctx, s) })
}

func (g *Gate) View(st *core.State, w, h int) *lipgloss.Layer {
	return g.stage.Layer(w, h, st.Now, func(maxH int) *ui.Window {
		p := ui.P()
		win := &ui.Window{W: ui.CardWidth(w), Title: "Welcome to dokky", Hints: ui.WindowKeys(g.Actions(st)), Active: 0,
			Buttons: []ui.Btn{{ID: "continue", Label: "Continue", Primary: true}}}
		cw, bg := win.Inner(), p.Window
		body := ui.NewBox()
		y := 0
		for i, c := range choices() {
			on := i == g.sel
			top := ui.RowLine(cw, on, bg, func(bg color.Color, iw int) string {
				l := ui.L(bg)
				ic := p.Muted
				if on {
					ic = p.Accent
				}
				if c.icon != "" {
					l.T(c.icon, ic).Sp(2)
				}
				if on {
					return l.B(c.title, p.Text).Pad(iw)
				}
				return l.T(c.title, p.Text).Pad(iw)
			})
			lead := 0
			if c.icon != "" {
				lead = ui.Width(c.icon) + 2
			}
			under := ui.RowLine(cw, on, bg, func(bg color.Color, iw int) string {
				return ui.L(bg).Sp(lead).T(c.sub, p.Muted).Pad(iw)
			})
			id := "opt:" + string(rune('0'+i))
			body.Hit(0, y, id, top).Hit(0, y+1, id, under)
			y += 3
		}
		switch {
		case g.err != "":
			body.Put(0, y, ui.L(bg).T(core.Fit(g.err, cw), p.Coral).Pad(cw))
			y++
		case st.Updating != "":
			body.Put(0, y, ui.L(bg).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T(st.Updating, p.Muted).Pad(cw))
			y++
		default:
			y--
		}
		win.Body = body
		win.Fit(y, maxH)
		return win
	})
}

func (g *Gate) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "↑↓", Label: "Choose"}, {Key: "esc", Label: "Quit"}}
}

func (g *Gate) Animating(st *core.State) bool { return g.stage.Moving() || st.Updating != "" }
