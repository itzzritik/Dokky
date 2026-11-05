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
	tileH   = 4
	panelH  = 10
	spaceW  = 37
	growFor = 1200 * time.Millisecond
)

type Overview struct {
	sel    int
	shown  time.Time
	scroll int
}

func New(now time.Time) *Overview { return &Overview{shown: now} }

// grid items: drives then "+ Add a drive", or the provider tiles when there are none
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
			out = append(out, item{id: "tile:" + d.ID, drive: d})
		}
		return append(out, item{id: "tile:add", adding: true})
	}
	cloud, own := groups(st)
	for i := range cloud {
		out = append(out, item{id: "tile:" + cloud[i].Type, prov: &cloud[i]})
	}
	out = append(out, item{id: "tile:more", more: true})
	for i := range own {
		out = append(out, item{id: "tile:" + own[i].Type, prov: &own[i]})
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

func cols(st *core.State, empty bool) int {
	switch {
	case !st.Wide:
		return 2
	case empty:
		return 4
	}
	return 3
}

func (o *Overview) Horizontal(*core.State) bool { return true }

func (o *Overview) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	items := o.items(st)
	o.sel = max(0, min(o.sel, len(items)-1))
	n := cols(st, len(st.Snap.Drives) == 0)
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
		case "left":
			o.sel = max(0, o.sel-1)
		case "right":
			o.sel = min(len(items)-1, o.sel+1)
		case "up":
			o.sel = max(0, o.sel-n)
		case "down":
			o.sel = min(len(items)-1, o.sel+n)
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
	acts := []core.Action{{Key: "↑↓←→", Label: "move"}}
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
	if h >= 3+panelH+1+1+tileH {
		aw := w
		if st.Wide {
			aw = w - spaceW - 2
			b.Put(aw+2, y, o.space(st, spaceW))
		}
		b.Put(0, y, activity(st, aw))
		y += panelH + 1
	}
	b.Put(0, y, ui.Paint("Your drives", p.Muted, nil, false))
	o.grid(st, b, y+1, w, h-y-1, false)
	return b.Layer()
}

// tiles in rows, scrolled so the selected one shows; rows that do not fit are left out
func (o *Overview) grid(st *core.State, b *ui.Box, top, w, h int, empty bool) {
	items := o.items(st)
	n := cols(st, empty)
	tw := (w - 3*(n-1)) / n
	o.tiles(st, b, items, 0, len(items), top, w, h, n, tw)
}

func (o *Overview) tiles(st *core.State, b *ui.Box, items []item, from, to, top, w, h, n, tw int) {
	rows := max(1, (h+1)/(tileH+1))
	row := (o.sel - from) / n
	if o.sel >= from && o.sel < to {
		if row < o.scroll {
			o.scroll = row
		}
		if row >= o.scroll+rows {
			o.scroll = row - rows + 1
		}
	}
	for i := from; i < to; i++ {
		r := (i-from)/n - o.scroll
		if r < 0 || r >= rows {
			continue
		}
		x, y := ((i-from)%n)*(tw+3), top+r*(tileH+1)
		b.Add(x, y, tile(st, items[i], tw, i == o.sel))
	}
}

func tile(st *core.State, it item, w int, sel bool) *lipgloss.Layer {
	p := ui.P()
	return ui.Tile(it.id, w, sel, func(bg color.Color, iw int) []string {
		switch {
		case it.drive != nil:
			d := *it.drive
			s := ui.DriveStatus(d, st.T)
			return []string{
				ui.L(bg).B(d.Name, p.Text).Right(iw, s.Line(bg, true)),
				ui.L(bg).T(kind(st, d), p.Muted).Pad(iw),
			}
		case it.prov != nil:
			hc := p.Muted
			if sel {
				hc = ui.Mix(p.Muted, p.Text, .4)
			}
			return []string{ui.L(bg).B(it.prov.Name, p.Text).Pad(iw), ui.L(bg).T(it.prov.Hint, hc).Pad(iw)}
		case it.more:
			return []string{ui.L(bg).B("More services", p.Text).Pad(iw), ui.L(bg).T("Search "+strconv.Itoa(max(0, len(st.Catalog)-11))+"+", p.Muted).Pad(iw)}
		}
		nc := p.Muted
		if sel {
			nc = p.Text
		}
		return []string{ui.L(bg).B("+ Add a drive", nc).Pad(iw), ui.L(bg).T(strconv.Itoa(len(st.Catalog))+" services", p.Faint).Pad(iw)}
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
