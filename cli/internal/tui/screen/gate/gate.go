package gate

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/brand"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var choices = []string{"Sign in with browser", "Use on this computer only"}

type Gate struct {
	mark *brand.Mark
	sel  int
	req  core.Req
	err  string
}

func New(now time.Time) *Gate { return &Gate{mark: brand.NewMark(now)} }

func (g *Gate) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "left", "right":
			g.sel = 1 - g.sel
		case "enter":
			return g, g.choose(st)
		}
	case core.Click:
		switch m.ID {
		case "btn:0", "btn:1":
			g.sel = int(m.ID[4] - '0')
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
	p := ui.P()
	b := ui.NewBox()
	y := max(1, h/2-9)
	b.Put((w-brand.W)/2, y, g.mark.View(st.Now))
	center := func(y int, s string) { b.Put((w-ui.Width(s))/2, y, s) }
	center(y+7, ui.Paint("Your drives, in one folder.", p.Muted, nil, false))
	bw := ui.ButtonWidth(choices[0]) + 2 + ui.ButtonWidth(choices[1])
	x := (w - bw) / 2
	for i, c := range choices {
		b.Add(x, y+10, ui.Button("btn:"+string(rune('0'+i)), c, i == 0, false, g.sel == i))
		x += ui.ButtonWidth(c) + 2
	}
	if g.err != "" {
		center(y+12, ui.Paint(core.Fit(g.err, w-4), p.Coral, nil, false))
	} else {
		center(y+12, ui.Paint("You can sign in later from Settings.", p.Faint, nil, false))
	}
	return b.Layer()
}

func (g *Gate) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}}
}

func (g *Gate) Animating(st *core.State) bool { return g.mark.Animating(st.Now) }
