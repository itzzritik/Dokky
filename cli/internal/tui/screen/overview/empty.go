package overview

import (
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the welcome: provider tiles in two groups, scrolled as one grid
func (o *Overview) empty(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	b.Put(0, 0, ui.Paint("Add your first drive", p.Text, nil, true))
	b.Put(0, 1, ui.Paint("Pick where your files live.", p.Muted, nil, false))
	items := o.items(st)
	cloud, _ := groups(st)
	split := len(cloud) + 1
	n := cols(st, true)
	tw := (w - 3*(n-1)) / n
	hint := 0
	if !st.Snap.Account.LoggedIn {
		hint = 2
	}
	room := h - 3 - hint
	cloudRows := (split + n - 1) / n
	ownRows := (len(items) - split + n - 1) / n
	need := 2 + (cloudRows+ownRows)*(tileH+1)
	if need <= room {
		b.Put(0, 3, ui.Paint("Cloud accounts", p.Muted, nil, false))
		o.scroll = 0
		o.tiles(st, b, items, 0, split, 4, w, cloudRows*(tileH+1), n, tw)
		y := 4 + cloudRows*(tileH+1)
		b.Put(0, y, ui.Paint("Your own storage", p.Muted, nil, false))
		o.scroll = 0
		o.tiles(st, b, items, split, len(items), y+1, w, ownRows*(tileH+1), n, tw)
		if hint > 0 {
			b.Put(0, y+1+ownRows*(tileH+1)+1, ui.Paint("Already use dokky on another computer?", p.Faint, nil, false))
		}
		return b.Layer()
	}
	label := "Cloud accounts"
	if o.sel >= split {
		label = "Your own storage"
	}
	b.Put(0, 3, ui.Paint(label, p.Muted, nil, false))
	from, to := 0, split
	if o.sel >= split {
		from, to = split, len(items)
	}
	o.tiles(st, b, items, from, to, 4, w, room-1, n, tw)
	if hint > 0 {
		b.Put(0, h-1, ui.Paint("Already use dokky on another computer?", p.Faint, nil, false))
	}
	return b.Layer()
}
