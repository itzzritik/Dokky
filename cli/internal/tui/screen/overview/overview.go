package overview

import (
	"image/color"
	"strconv"
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
	cardH  = 5
	panelH = 10
	stackH = 15 // taller when the files sit under the graph
)

type Overview struct {
	sel, top int
	shown    time.Time
}

func New() *Overview { return &Overview{} }

// list items: the drives, or the services when there are none
type item struct {
	id    string
	drive *api.Drive
	prov  *api.Provider
	more  bool
}

func (o *Overview) items(st *core.State) []item {
	var out []item
	if len(st.Snap.Drives) > 0 {
		for i := range st.Snap.Drives {
			d := &st.Snap.Drives[i]
			out = append(out, item{id: "row:" + d.ID, drive: d})
		}
		return out
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
		return core.Send(core.SwitchTab{Tab: core.DrivesTab, Drive: it.drive.ID})
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
			acts = append(acts, core.Action{Key: "enter", Label: "show"})
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
	if ui.Showing(st.Act.History) || ui.MeterGrowing(o.shown, st.Now) {
		return len(st.Snap.Drives) > 0
	}
	for _, d := range st.Snap.Drives {
		if ui.MeterMoving() && d.Space != nil && d.Space.Total > d.Space.Free {
			return true
		}
	}
	return false
}

func (o *Overview) View(st *core.State, w, h int) *lipgloss.Layer {
	if len(st.Snap.Drives) == 0 {
		return o.empty(st, w, h)
	}
	p := ui.P()
	b := ui.NewBox()
	y := 0
	ph := panelH
	if w < splitW && h >= y+stackH+1+1+2*cardH {
		ph = stackH
	}
	if h >= y+ph+1+1+2*cardH {
		b.Put(0, y, activity(st, w, ph))
		y += ph + 1
	}
	b.Put(0, y, ui.Paint("Your drives", p.Muted, nil, false))
	items := o.items(st)
	fit := max(1, (h-y-1)/cardH)
	o.top = max(0, min(o.top, len(items)-fit))
	if o.sel < o.top {
		o.top = o.sel
	}
	if o.sel >= o.top+fit {
		o.top = o.sel - fit + 1
	}
	if o.shown.IsZero() {
		o.shown = st.Now
	}
	grow := ui.MeterGrow(o.shown, st.Now)
	for i := o.top; i < len(items) && i < o.top+fit; i++ {
		b.Add(0, y+1+(i-o.top)*cardH, card(st, items[i], w, i == o.sel, grow))
	}
	return b.Layer()
}

// a drive as a solid card: name, what it is and its status on the left, its space meter on the right
func card(st *core.State, it item, w int, sel bool, grow float64) *lipgloss.Layer {
	p := ui.P()
	return ui.Tile(it.id, w, sel, func(bg color.Color, iw int) []string {
		d := *it.drive
		mw := iw / 2
		lw := iw - mw - 3
		about := ui.L(bg).T(kind(st, d), p.Muted)
		if d.Account != "" && d.Account != d.ServiceName {
			about.T("  ·  "+d.Account, p.Faint)
		}
		status := ui.DriveStatus(d, st.T).Line(bg, true)
		if d.Uploads > 0 {
			status.Sp(2).T(core.Count(d.Uploads, "upload")+" waiting", p.Accent)
		}
		left := []string{ui.L(bg).B(d.Name, p.Text).Pad(lw), about.Pad(lw), status.Pad(lw)}
		meter := ui.SpaceMeter(mw, d, st.T, grow, bg)
		rows := make([]string, 3)
		for i := range rows {
			rows[i] = ui.L(bg).Raw(left[i]).Sp(3).Raw(meter[i]).String()
		}
		return rows
	})
}

// a service in the welcome list
func row(st *core.State, it item, w int, sel bool) string {
	p := ui.P()
	return ui.RowLine(w, sel, nil, func(bg color.Color, iw int) string {
		name, hint := "More services", "Search "+strconv.Itoa(max(0, len(st.Catalog)-11))+"+"
		if it.prov != nil {
			name, hint = it.prov.Name, it.prov.Desc
		}
		l := ui.L(bg)
		if sel {
			l.B(name, p.Text)
		} else {
			l.T(name, p.Text)
		}
		nw := min(30, iw/3)
		return ui.L(bg).Raw(l.Pad(nw)).Sp(2).T(core.Fit(hint, iw-nw-2), p.Muted).Pad(iw)
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
