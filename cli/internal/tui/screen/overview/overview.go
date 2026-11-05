package overview

import (
	"image/color"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/add"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	panelH  = 10
	spaceW  = 37
	growFor = 1200 * time.Millisecond
)

type Overview struct {
	sel, top int
	shown    time.Time
}

func New(now time.Time) *Overview { return &Overview{shown: now} }

// list items: drives then "+ Add a drive", or the services when there are none
type item struct {
	id     string
	drive  *api.Drive
	prov   *api.Provider
	more   bool
	adding bool
}

func (o *Overview) items(st *core.State) []item {
	var out []item
	if len(st.Snap.Drives) > 0 {
		for i := range st.Snap.Drives {
			d := &st.Snap.Drives[i]
			out = append(out, item{id: "row:" + d.ID, drive: d})
		}
		return append(out, item{id: "row:add", adding: true})
	}
	cloud, own := groups(st)
	for i := range cloud {
		out = append(out, item{id: "row:" + cloud[i].Type, prov: &cloud[i]})
	}
	out = append(out, item{id: "row:more", more: true})
	for i := range own {
		out = append(out, item{id: "row:" + own[i].Type, prov: &own[i]})
	}
	return out
}

func groups(st *core.State) (cloud, own []api.Provider) {
	for _, p := range st.Catalog {
		switch {
		case !p.Popular:
		case p.Group == "own":
			own = append(own, p)
		default:
			cloud = append(cloud, p)
		}
	}
	return cloud, own
}

func (o *Overview) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	items := o.items(st)
	o.sel = max(0, min(o.sel, len(items)-1))
	switch m := msg.(type) {
	case core.SwitchTab:
		if m.Tab == 0 {
			o.shown = st.Now
		}
	case core.Click:
		for i, it := range items {
			if it.id == m.ID {
				o.sel = i
				return o, o.activate(st, it)
			}
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "up":
			o.sel = max(0, o.sel-1)
		case "down":
			o.sel = min(len(items)-1, o.sel+1)
		case "enter":
			if len(items) > 0 {
				return o, o.activate(st, items[o.sel])
			}
		case "a":
			return o, add.Open(st, "")
		case "c":
			if d := items[o.sel].drive; d != nil && d.State == api.Connected {
				return o, core.Disconnect(st, d.ID)
			}
		case "s":
			if !st.Snap.Account.LoggedIn {
				return o, gate.Open(st)
			}
		}
	}
	return o, nil
}

func (o *Overview) activate(st *core.State, it item) tea.Cmd {
	switch {
	case it.drive != nil:
		return core.RunPrimary(st, *it.drive)
	case it.prov != nil:
		return add.Open(st, it.prov.Type)
	}
	return add.Open(st, "")
}

func (o *Overview) Actions(st *core.State) []core.Action {
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	items := o.items(st)
	if o.sel < len(items) {
		switch it := items[o.sel]; {
		case it.drive != nil:
			acts = append(acts, core.DriveActions(*it.drive)...)
		default:
			acts = append(acts, core.Action{Key: "enter", Label: "add"})
		}
	}
	if len(st.Snap.Drives) > 0 {
		acts = append(acts, core.Action{Key: "a", Label: "add drive"})
	}
	if !st.Snap.Account.LoggedIn {
		acts = append(acts, core.Action{Key: "s", Label: "sign in"})
	}
	return acts
}

func (o *Overview) Animating(st *core.State) bool {
	if ui.Reduced() {
		return false
	}
	for _, d := range st.Snap.Drives {
		if d.State == api.Connecting {
			return true
		}
	}
	return len(st.Snap.Drives) > 0 && (st.Act.Down > 0 || st.Act.Up > 0 || ui.Easing(o.shown, st.Now, growFor))
}

func (o *Overview) View(st *core.State, w, h int) *lipgloss.Layer {
	if len(st.Snap.Drives) == 0 {
		return o.empty(st, w, h)
	}
	p := ui.P()
	b := ui.NewBox()
	on, uploads, needs := 0, 0, 0
	for _, d := range st.Snap.Drives {
		uploads += d.Uploads
		switch d.State {
		case api.Connected:
			on++
		case api.Failed, api.Reconnect:
			needs++
		}
	}
	b.Put(0, 0, ui.Paint(strconv.Itoa(on)+" of "+strconv.Itoa(len(st.Snap.Drives))+" drives connected", p.Text, nil, true))
	var says []string
	if uploads > 0 {
		says = append(says, strconv.Itoa(uploads)+" uploading.")
	}
	if needs > 0 {
		says = append(says, strconv.Itoa(needs)+" needs you.")
	}
	if len(says) == 0 {
		says = append(says, "All quiet.")
	}
	b.Put(0, 1, ui.Paint(strings.Join(says, " "), p.Muted, nil, false))
	y := 3
	if h >= 3+panelH+1+1+3 {
		aw := w
		if st.Wide {
			aw = w - spaceW - 2
			b.Put(aw+2, y, o.space(st, spaceW))
		}
		b.Put(0, y, activity(st, aw))
		y += panelH + 1
	}
	var lines []ui.ListLine
	lines = append(lines, ui.ListLine{Text: ui.Paint("Your drives", p.Muted, nil, false), Item: -1})
	for i, it := range o.items(st) {
		lines = append(lines, ui.ListLine{Text: row(st, it, w, i == o.sel), Item: i, ID: it.id})
	}
	ui.List(b, y, h-y, &o.top, o.sel, lines)
	return b.Layer()
}

func row(st *core.State, it item, w int, sel bool) string {
	p := ui.P()
	return ui.RowLine(w, sel, nil, func(bg color.Color, iw int) string {
		name := func(s string, c color.Color) *ui.Line {
			if sel {
				return ui.L(bg).B(s, c)
			}
			return ui.L(bg).T(s, c)
		}
		col := min(30, iw/3)
		switch {
		case it.drive != nil:
			d := *it.drive
			return ui.L(bg).Raw(name(d.Name, p.Text).Pad(col)).Sp(2).T(kind(st, d), p.Muted).Right(iw, ui.DriveStatus(d, st.T).Line(bg, true))
		case it.prov != nil:
			return ui.L(bg).Raw(name(it.prov.Name, p.Text).Pad(col)).Sp(2).T(it.prov.Hint, p.Muted).Pad(iw)
		case it.more:
			return ui.L(bg).Raw(name("More services", p.Text).Pad(col)).Sp(2).T("Search "+strconv.Itoa(max(0, len(st.Catalog)-11))+"+", p.Muted).Pad(iw)
		}
		nc := p.Muted
		if sel {
			nc = p.Text
		}
		return ui.L(bg).Raw(name("+ Add a drive", nc).Pad(col)).Sp(2).T(strconv.Itoa(len(st.Catalog))+" services", p.Faint).Pad(iw)
	})
}

func kind(st *core.State, d api.Drive) string {
	for _, p := range st.Catalog {
		if p.Type == d.ServiceType {
			return p.Name
		}
	}
	return d.ServiceType
}
