package add

import (
	"context"
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type picker struct {
	search   *ui.Field
	accounts []api.AccountRef
	req      core.Req
	all      bool
	sel, top int
}

type option struct {
	name, hint string
	prov       api.Provider
	acct       *api.AccountRef
	more       bool
}

func (p *picker) load(st *core.State) tea.Cmd {
	p.req = core.NextReq()
	return core.Do(st, p.req, func(ctx context.Context, c *client.Client) (any, error) { return c.Accounts(ctx) })
}

func (p *picker) loading() bool { return p.req != 0 }

func (p *picker) options(st *core.State) []option {
	q := strings.ToLower(strings.TrimSpace(p.search.Value()))
	match := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	var out []option
	for i := range p.accounts {
		acct := &p.accounts[i]
		prov, _ := provider(st, acct.Type)
		hint := "Your account"
		if acct.Drives > 0 {
			hint = "Your account, " + core.Count(acct.Drives, "drive")
		}
		if match(acct.Name) || match(prov.Name) {
			out = append(out, option{name: acct.Name, hint: hint, prov: prov, acct: acct})
		}
	}
	more := 0
	for _, pr := range st.Catalog {
		if !pr.Popular && !p.all && q == "" {
			more++
			continue
		}
		if match(pr.Name) || match(pr.Type) {
			out = append(out, option{name: pr.Name, hint: pr.Hint, prov: pr})
		}
	}
	if more > 0 {
		out = append(out, option{name: "All services", hint: strconv.Itoa(more) + " more", more: true})
	}
	return out
}

func (a *Add) updateChoose(msg tea.Msg, st *core.State) tea.Cmd {
	p := &a.pick
	opts := p.options(st)
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "row:")); err == nil && i < len(opts) {
			p.sel = i
			return a.pickOption(st, opts[i])
		}
		return nil
	case tea.KeyPressMsg:
		switch m.String() {
		case "up":
			p.sel = max(0, p.sel-1)
			return nil
		case "down":
			p.sel = min(len(opts)-1, p.sel+1)
			return nil
		case "enter":
			if p.sel < len(opts) {
				return a.pickOption(st, opts[p.sel])
			}
			return nil
		}
	}
	before := p.search.Value()
	cmd, _ := p.search.Type(msg)
	if p.search.Value() != before {
		p.sel, p.top = 0, 0
	}
	return cmd
}

func (a *Add) pickOption(st *core.State, o option) tea.Cmd {
	if o.more {
		a.pick.all = true
		return nil
	}
	return a.choose(st, o.prov, o.acct)
}

func (a *Add) viewChoose(st *core.State, win *ui.Window, maxH int) int {
	pal := ui.P()
	p := &a.pick
	win.Title, win.Sub = "Where do your files live?", "Pick a service."
	cw := win.Inner()
	body := ui.NewBox()
	body.Add(0, 0, p.search.View("search", cw, true, pal.Surface))
	opts := p.options(st)
	room := max(3, min(10, maxH-7-2-2))
	p.sel = max(0, min(p.sel, len(opts)-1))
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+room {
		p.top = p.sel - room + 1
	}
	rows := 0
	for i := p.top; i < len(opts) && i < p.top+room; i++ {
		o := opts[i]
		body.Add(0, 2+rows, ui.Row("row:"+strconv.Itoa(i), cw, i == p.sel, pal.Surface, func(bg color.Color, iw int) string {
			nc, hc := ui.Mix(pal.Muted, pal.Text, .6), pal.Faint
			if i == p.sel {
				nc, hc = pal.Text, pal.Muted
			}
			l := ui.L(bg)
			if i == p.sel {
				l.B(o.name, nc)
			} else {
				l.T(o.name, nc)
			}
			return ui.L(bg).Raw(l.Pad(20)).T(o.hint, hc).Pad(iw)
		}))
		rows++
	}
	if len(opts) == 0 {
		body.Put(0, 2, win.Text("Nothing matches. Try another word.", pal.Faint))
		rows = 1
	}
	win.Body = body
	return 2 + rows
}
