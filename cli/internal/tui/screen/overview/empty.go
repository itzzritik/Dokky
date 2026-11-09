package overview

import (
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the welcome: services in two groups, one list
func (o *Overview) empty(st *core.State, w, h int) *lipgloss.Layer {
	p := ui.P()
	b := ui.NewBox()
	b.Put(0, 0, ui.Paint("Add your first drive", p.Text, nil, true))
	b.Put(0, 1, ui.Paint("Pick where your files live.", p.Muted, nil, false))
	cloud, _ := groups(st)
	header := func(s string) ui.ListLine { return ui.ListLine{Text: ui.Paint(s, p.Muted, nil, false), Item: -1} }
	lines := []ui.ListLine{header("Cloud accounts")}
	items := o.items(st)
	o.current(items)
	for i, it := range items {
		switch {
		case i == len(cloud)+1 && !it.signIn:
			lines = append(lines, ui.ListLine{Item: -1}, header("Your own storage"))
		case it.signIn:
			lines = append(lines, ui.ListLine{Item: -1})
		}
		lines = append(lines, ui.ListLine{Text: row(st, it, w, i == o.sel), Item: i, ID: it.id})
	}
	room := h - 3
	o.page = max(1, room-2)
	ui.List(b, 3, room, &o.top, o.sel, lines)
	return b.Layer()
}
