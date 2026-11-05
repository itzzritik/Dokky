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
	cardH   = 4
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
		case "r":
			var cmds []tea.Cmd
			for _, d := range needy(st) {
				cmds = append(cmds, core.RunPrimary(st, d))
			}
			return o, tea.Batch(cmds...)
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
	if len(needy(st)) > 0 {
		acts = append(acts, core.Action{Key: "r", Label: "reconnect"})
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
	return len(st.Snap.Drives) > 0 && (ui.Showing(st.Act.History) || ui.Easing(o.shown, st.Now, growFor))
}

func (o *Overview) View(st *core.State, w, h int) *lipgloss.Layer {
	if len(st.Snap.Drives) == 0 {
		return o.empty(st, w, h)
	}
	p := ui.P()
	b := ui.NewBox()
	y := 3
	if needs := needy(st); len(needs) > 0 {
		bh := 1
		if h >= 21 {
			bh = 3
		}
		b.Lines(0, 0, banner(needs, w, bh))
		y = bh + 1
	} else {
		on, uploads := 0, 0
		for _, d := range st.Snap.Drives {
			uploads += d.Uploads
			if d.State == api.Connected {
				on++
			}
		}
		b.Put(0, 0, ui.Paint(strconv.Itoa(on)+" of "+strconv.Itoa(len(st.Snap.Drives))+" drives connected", p.Text, nil, true))
		says := "All quiet."
		if uploads > 0 {
			says = strconv.Itoa(uploads) + " uploading."
		}
		b.Put(0, 1, ui.Paint(says, p.Muted, nil, false))
	}
	if h >= y+panelH+1+1+2*cardH {
		aw := w
		if st.Wide {
			aw = w - spaceW - 2
			b.Put(aw+2, y, o.space(st, spaceW))
		}
		b.Put(0, y, activity(st, aw))
		y += panelH + 1
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
	for i := o.top; i < len(items) && i < o.top+fit; i++ {
		b.Add(0, y+1+(i-o.top)*cardH, card(st, items[i], w, i == o.sel))
	}
	return b.Layer()
}

// drives that can't connect or wait for a reconnect
func needy(st *core.State) []api.Drive {
	var out []api.Drive
	for _, d := range st.Snap.Drives {
		if d.State == api.Failed || d.State == api.Reconnect {
			out = append(out, d)
		}
	}
	return out
}

func banner(needs []api.Drive, w, h int) []string {
	bad := false
	for _, d := range needs {
		bad = bad || d.State == api.Failed
	}
	text := core.Count(len(needs), "drive") + " need you"
	if len(needs) == 1 {
		text = needs[0].Name + " can't connect"
		if !bad {
			text = needs[0].Name + " needs a reconnect"
		}
	}
	return ui.Banner(w, h, bad, text, "r", "reconnect")
}

// a drive as a solid card: name and status, then what it is and what it is doing
func card(st *core.State, it item, w int, sel bool) *lipgloss.Layer {
	p := ui.P()
	return ui.Tile(it.id, w, sel, func(bg color.Color, iw int) []string {
		if it.drive == nil {
			nc := p.Muted
			if sel {
				nc = p.Text
			}
			return []string{ui.L(bg).B("+ Add a drive", nc).Pad(iw), ui.L(bg).T(strconv.Itoa(len(st.Catalog))+" services", p.Faint).Pad(iw)}
		}
		d := *it.drive
		about := ui.L(bg).T(kind(st, d), p.Muted)
		if d.Account != "" && d.Account != d.ServiceName {
			about.T("  ·  "+d.Account, p.Faint)
		}
		right := ui.L(bg)
		switch {
		case d.Uploads > 0:
			right.T(core.Count(d.Uploads, "upload")+" waiting", p.Accent)
		case d.Space != nil && d.Space.Total > 0:
			right.T(core.Size(d.Space.Free)+" free", p.Faint)
		}
		return []string{
			ui.L(bg).B(d.Name, p.Text).Right(iw, ui.DriveStatus(d, st.T).Line(bg, true)),
			about.Right(iw, right),
		}
	})
}

// a service in the welcome list
func row(st *core.State, it item, w int, sel bool) string {
	p := ui.P()
	return ui.RowLine(w, sel, nil, func(bg color.Color, iw int) string {
		name, hint := "More services", "Search "+strconv.Itoa(max(0, len(st.Catalog)-11))+"+"
		if it.prov != nil {
			name, hint = it.prov.Name, it.prov.Hint
		}
		l := ui.L(bg)
		if sel {
			l.B(name, p.Text)
		} else {
			l.T(name, p.Text)
		}
		return ui.L(bg).Raw(l.Pad(min(30, iw/3))).Sp(2).T(hint, p.Muted).Pad(iw)
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
