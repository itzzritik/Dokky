package vault

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type choice struct {
	label, note string
	off         string // why it can't be picked on this computer, "" when it can
}

// one security setting's choices; picking another asks for Touch ID or the password, then saves
type pick struct {
	title, sub string
	opts       []choice
	sel, cur   int
	err        string
	save       func(*core.State, int) tea.Cmd
}

func (pk *pick) update(msg tea.Msg, f *Flow, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "opt:")); err == nil && i < len(pk.opts) {
			pk.sel = i
			return pk.choose(f, st)
		}
	case tea.KeyPressMsg:
		switch m.String() {
		case "esc":
			return f.back()
		case "up":
			pk.sel, pk.err = max(0, pk.sel-1), ""
		case "down":
			pk.sel, pk.err = min(len(pk.opts)-1, pk.sel+1), ""
		case "enter", "space":
			return pk.choose(f, st)
		}
	}
	return nil
}

func (pk *pick) choose(f *Flow, st *core.State) tea.Cmd {
	switch o := pk.opts[pk.sel]; {
	case pk.sel == pk.cur:
		return f.back()
	case o.off != "":
		pk.err = o.off
		return nil
	}
	return tea.Batch(core.Close(f), pk.save(st, pk.sel))
}

func (pk *pick) view(_ *core.State, win *ui.Window, maxH int) int {
	p := ui.P()
	win.Title = pk.title
	sub(win, pk.sub, pk.err)
	cw, bg := win.Inner(), p.Window
	var lines []ui.ListLine
	for i, o := range pk.opts {
		row := ui.RowLine(cw, i == pk.sel, bg, func(bg color.Color, iw int) string {
			mark, mc, lc := ui.G.Ring, p.Faint, p.Text
			if i == pk.cur {
				mark, mc = ui.G.Here, p.Accent
			}
			note, nc := o.note, p.Muted
			if o.off != "" {
				lc, note, nc = p.Faint, o.off, p.Faint
			}
			l := ui.L(bg).B(mark, mc).Sp(1)
			if i == pk.sel {
				l.B(o.label, lc)
			} else {
				l.T(o.label, lc)
			}
			return l.Right(iw-1, ui.L(bg).T(note, nc))
		})
		lines = append(lines, ui.ListLine{Text: row, Item: i, ID: "opt:" + strconv.Itoa(i)})
	}
	rows := min(len(lines), win.Room(maxH))
	body := ui.NewBox()
	top := 0
	ui.List(body, 0, rows, &top, pk.sel, lines)
	win.Body = body
	return rows
}

func (pk *pick) actions(*core.State) []core.Action {
	return []core.Action{{Key: "↑↓", Label: "move"}, {Key: "enter", Label: "choose"}}
}

func (pk *pick) typing() bool               { return false }
func (pk *pick) holding() bool              { return false }
func (pk *pick) animating(*core.State) bool { return false }
func (pk *pick) wipe()                      {}
