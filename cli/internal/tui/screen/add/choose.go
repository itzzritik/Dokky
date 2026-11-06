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
	sel, top int
}

type option struct {
	name, desc string
	prov       api.Provider
	acct       *api.AccountRef
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
		desc := "Reuse your " + cmp.Or(prov.Name, acct.Type) + " sign-in, no need to sign in again."
		if acct.Drives > 0 {
			desc = "Reuse your " + cmp.Or(prov.Name, acct.Type) + " sign-in, already used by " + core.Count(acct.Drives, "drive") + "."
		}
		if match(acct.Name) || match(prov.Name) {
			out = append(out, option{name: acct.Name, desc: desc, prov: prov, acct: acct})
		}
	}
	for _, pr := range st.Catalog {
		if match(pr.Name) || match(pr.Type) || match(pr.Desc) {
			out = append(out, option{name: pr.Name, desc: pr.Desc, prov: pr})
		}
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
			return a.choose(st, opts[i].prov, opts[i].acct)
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
				return a.choose(st, opts[p.sel].prov, opts[p.sel].acct)
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

// rows under the list: a gap, a rule, a gap and one line of description
const descRows = 4

func (a *Add) viewChoose(st *core.State, win *ui.Window, maxH int) int {
	pal := ui.P()
	p := &a.pick
	win.Title, win.Sub = "Add drive", "Where do your files live?"
	cw := win.Inner()
	body := ui.NewBox()
	body.Add(0, 0, p.search.View("search", cw, true, pal.Surface))
	opts := p.options(st)
	if len(opts) == 0 {
		body.Put(0, 2, win.Text("Nothing matches. Try another word.", pal.Faint))
		win.Body = body
		return 3
	}
	lines := grouped(opts)
	room := max(3, min(14, win.Room(maxH)-2-descRows))
	p.sel = max(0, min(p.sel, len(opts)-1))
	at := 0
	for j, l := range lines {
		if l.opt == p.sel {
			at = j
		}
	}
	// a group's first option brings its heading into view, and the list never starts on the gap between groups
	head := at
	for head > 0 && lines[head-1].opt < 0 && lines[head-1].head != "" {
		head--
	}
	if head < p.top {
		p.top = head
	}
	if at >= p.top+room {
		p.top = at - room + 1
	}
	if l := lines[p.top]; l.opt < 0 && l.head == "" {
		p.top++
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
			if i == p.sel {
				return ui.L(bg).B(o.name, pal.Text).Pad(iw)
			}
			return ui.L(bg).T(o.name, ui.Mix(pal.Muted, pal.Text, .6)).Pad(iw)
		}))
		rows++
	}
	y := 2 + rows + 1
	body.Put(0, y, ui.L(pal.Surface).T(strings.Repeat(ui.G.H, cw), ui.Mix(pal.Surface, pal.Text, .1)).Pad(cw))
	body.Put(0, y+2, ui.L(pal.Surface).Sp(2).T(core.Fit(opts[p.sel].desc, cw-2), pal.Muted).Pad(cw))
	win.Body = body
	return y + descRows - 1
}
