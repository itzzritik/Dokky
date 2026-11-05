package add

import (
	"cmp"
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
		hint := cmp.Or(prov.Name, acct.Type)
		if acct.Drives > 0 {
			hint += ", " + core.Count(acct.Drives, "drive")
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

// a group heading, a gap, or an option
type line struct {
	head string
	opt  int
}

// accounts already signed in and new services sit under their own headings, when there are both
func grouped(opts []option) []line {
	var out []line
	mixed := len(opts) > 0 && opts[0].acct != nil && opts[len(opts)-1].acct == nil
	for i, o := range opts {
		switch {
		case !mixed:
		case i == 0:
			out = append(out, line{head: "Your accounts", opt: -1})
		case o.acct == nil && opts[i-1].acct != nil:
			out = append(out, line{opt: -1}, line{head: "New account", opt: -1})
		}
		out = append(out, line{opt: i})
	}
	return out
}

func (a *Add) viewChoose(st *core.State, win *ui.Window, maxH int) int {
	pal := ui.P()
	p := &a.pick
	win.Title, win.Sub = "Add drive", "Where do your files live?"
	cw := win.Inner()
	body := ui.NewBox()
	body.Add(0, 0, p.search.View("search", cw, true, pal.Surface))
	opts := p.options(st)
	lines := grouped(opts)
	room := max(3, min(14, win.Room(maxH)-2))
	p.sel = max(0, min(p.sel, len(opts)-1))
	at := 0
	for j, l := range lines {
		if l.opt == p.sel {
			at = j
		}
	}
	// a group's first option brings its heading into view
	head := at
	for head > 0 && lines[head-1].opt < 0 {
		head--
	}
	if head < p.top {
		p.top = head
	}
	if at >= p.top+room {
		p.top = at - room + 1
	}
	rows := 0
	for j := p.top; j < len(lines) && j < p.top+room; j++ {
		l := lines[j]
		if l.opt < 0 {
			body.Put(0, 2+rows, ui.L(pal.Surface).Sp(2).T(l.head, pal.Faint).Pad(cw))
			rows++
			continue
		}
		i, o := l.opt, opts[l.opt]
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
