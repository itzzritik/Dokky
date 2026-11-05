package add

import (
	"cmp"
	"context"
	"image/color"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the edit window's rows; the cache ones sit under their own heading
const (
	eName = iota
	eSource
	eMount
	eLogin
	eSize
	eAge
)

const editLabelW = 20

var ageLabels = map[string]string{"5m": "5 minutes", "15m": "15 minutes", "1h": "1 hour", "6h": "6 hours", "24h": "1 day", "7d": "7 days"}

// a list of values: enter edits one in place and saves it on its own; actions are keys in the footer
type editForm struct {
	focus      int
	field      *ui.Field // the value being typed, nil when only moving
	age        string    // a keep-for picked with the arrows, saved on enter
	req        core.Req
	err        string
	remounting bool
}

func editRows() []int { return []int{eName, eSource, eMount, eLogin, eSize, eAge} }

// the drive as the daemon has it now, not as the window opened
func (a *Add) live(st *core.State) api.Drive {
	if d, ok := st.Drive(a.edit.ID); ok {
		return d
	}
	return *a.edit
}

func (a *Add) connLabel(st *core.State) string {
	switch a.live(st).State {
	case api.Connected:
		return "disconnect"
	case api.Off, api.Failed:
		return "connect"
	}
	return ""
}

func (a *Add) canSignIn() bool { return a.edit.ServiceType != "" && !oauthType(a.edit.ServiceType) }

func (a *Add) updateEdit(msg tea.Msg, st *core.State) tea.Cmd {
	e := &a.ed
	d := a.live(st)
	rows := editRows()
	e.focus = max(0, min(e.focus, len(rows)-1))
	row := rows[e.focus]
	switch m := msg.(type) {
	case core.Reply:
		if m.Req != e.req {
			return nil
		}
		e.req, e.age, e.remounting = 0, "", false
		if m.Err != nil {
			e.err = core.Report(m.Err)
			return nil
		}
		if renamed, _ := m.Val.(bool); renamed {
			return tea.Batch(core.Say("Saved"), core.Send(core.Recheck{}))
		}
		return core.Say("Saved")
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "item:")); err == nil && e.field == nil {
			e.focus = slices.Index(rows, i)
			if i == eLogin {
				return a.saveEdit(st, api.DriveUpdate{AtLogin: ptr(!d.AtLogin)}, false)
			}
		}
		return nil
	case tea.KeyPressMsg:
		if e.req != 0 {
			return nil
		}
		k := m.String()
		if e.field != nil {
			switch k {
			case "esc":
				e.field = nil
				return nil
			case "enter":
				return a.commit(st, row, d)
			}
			cmd, typed := e.field.Type(m)
			if typed {
				e.err = ""
			}
			return cmd
		}
		switch k {
		case "up":
			e.focus = max(0, e.focus-1)
		case "down":
			e.focus = min(len(rows)-1, e.focus+1)
		case "enter", "space", "left", "right":
			switch {
			case row == eLogin:
				return a.saveEdit(st, api.DriveUpdate{AtLogin: ptr(!d.AtLogin)}, false)
			case row == eAge && (k == "left" || k == "right"):
				ages := engine.CacheMaxAges
				i := slices.Index(ages, cmp.Or(e.age, d.CacheMaxAge))
				if k == "left" {
					i = max(0, i-1)
				} else {
					i = min(len(ages)-1, i+1)
				}
				e.age = ages[i]
			case row == eAge:
				if e.age != "" && e.age != d.CacheMaxAge {
					return a.remountSave(st, d, api.DriveUpdate{CacheMaxAge: e.age}, false)
				}
			case k == "enter" && row != eAge:
				e.err = ""
				e.field = ui.NewField("", a.editValue(row, d), a.editHint(row, d), false)
				e.field.Bare = true
			}
		case "c":
			switch a.connLabel(st) {
			case "disconnect":
				return tea.Batch(core.Close(a), core.Disconnect(st, d.ID))
			case "connect":
				return tea.Batch(core.Close(a), core.Connect(st, d.ID))
			}
		case "s":
			if a.canSignIn() {
				return a.toSignIn(st)
			}
		case "d":
			return a.remove()
		}
	}
	return nil
}

// what an edit starts from, and the faint text an empty value shows
func (a *Add) editValue(row int, d api.Drive) string {
	switch row {
	case eName:
		return d.Name
	case eSource:
		return d.SubPath
	case eMount:
		return d.LocalFolder
	case eSize:
		return d.CacheMaxSize
	}
	return ""
}

func (a *Add) editHint(row int, d api.Drive) string {
	switch row {
	case eSource:
		return "Whole account"
	case eMount:
		return core.Home(d.Folder)
	case eSize:
		return "Like 50G"
	}
	return ""
}

func (a *Add) commit(st *core.State, row int, d api.Drive) tea.Cmd {
	v := strings.TrimSpace(a.ed.field.Value())
	a.ed.field = nil
	if v == a.editValue(row, d) {
		return nil
	}
	switch row {
	case eName:
		if v == "" {
			a.ed.err = "Give the drive a name."
			return nil
		}
		return a.remountSave(st, d, api.DriveUpdate{Name: v}, true)
	case eSource:
		v = strings.Trim(v, "/")
		return a.remountSave(st, d, api.DriveUpdate{SubPath: &v}, false)
	case eMount:
		return a.remountSave(st, d, api.DriveUpdate{LocalFolder: &v}, false)
	case eSize:
		return a.remountSave(st, d, api.DriveUpdate{CacheMaxSize: v}, false)
	}
	return nil
}

// a connected drive takes these changes by remounting, so it says so before saving; the daemon does the remount
func (a *Add) remountSave(st *core.State, d api.Drive, u api.DriveUpdate, renamed bool) tea.Cmd {
	if d.State != api.Connected {
		return a.saveEdit(st, u, renamed)
	}
	name := core.Safe(d.Name)
	return confirm.Open(&confirm.Confirm{
		Question: "Save and remount " + name + "?",
		Line:     name + " disconnects for a moment to remount.",
		Do:       "Save",
		Positive: true,
		Run: func(st *core.State, _ bool) tea.Cmd {
			a.ed.remounting = true
			return a.saveEdit(st, u, renamed)
		},
	})
}

func (a *Add) saveEdit(st *core.State, u api.DriveUpdate, renamed bool) tea.Cmd {
	e := &a.ed
	u.ID, e.req, e.err = a.edit.ID, core.NextReq(), ""
	return core.Do(st, e.req, func(ctx context.Context, c *client.Client) (any, error) {
		_, err := c.UpdateDrive(ctx, u)
		return renamed, err
	})
}

func ptr[T any](v T) *T { return &v }

func (a *Add) viewEdit(st *core.State, win *ui.Window) int {
	p := ui.P()
	e := &a.ed
	d := a.live(st)
	cw := win.Inner()
	win.Title = d.Name
	win.Sub = cmp.Or(a.prov.Name, d.ServiceType)
	if k := d.AccountDrives; k > 1 {
		win.Sub += ", also used by " + core.Count(k-1, "more drive")
	}
	switch {
	case e.req != 0 && e.remounting:
		win.Sub = ui.Spin(st.T) + " Saving and remounting"
	case e.req != 0:
		win.Sub = ui.Spin(st.T) + " Saving"
	case e.err != "":
		win.Sub, win.SubBad = e.err, true
	}
	rows := editRows()
	cur := rows[max(0, min(e.focus, len(rows)-1))]
	body := ui.NewBox()
	line := func(y, row int, label string, value func(bg color.Color, w int) string) {
		body.Add(0, y, ui.Row("item:"+strconv.Itoa(row), cw, row == cur, p.Surface, func(bg color.Color, iw int) string {
			l := ui.L(bg)
			if row == cur {
				l.B(label, p.Text)
			} else {
				l.T(label, ui.Mix(p.Muted, p.Text, .55))
			}
			vw := iw - editLabelW
			if row == cur && e.field != nil {
				return ui.L(bg).Raw(l.Pad(editLabelW)).Raw(ui.L(bg).Raw(e.field.View("edit", vw, true, bg).GetContent()).Pad(vw)).String()
			}
			return ui.L(bg).Raw(l.Pad(editLabelW)).Raw(value(bg, vw)).String()
		}))
	}
	text := func(v, empty string) func(bg color.Color, w int) string {
		return func(bg color.Color, w int) string {
			fg := p.Text
			if v == "" {
				v, fg = empty, p.Faint
			}
			return ui.L(bg).Right(w, ui.L(bg).T(core.Fit(v, w-1), fg))
		}
	}
	line(0, eName, "Name", text(d.Name, ""))
	line(1, eSource, "Source folder", text(d.SubPath, "Whole account"))
	line(2, eMount, "Mount folder", text(d.LocalFolder, core.Home(d.Folder)))
	line(3, eLogin, "Connect at login", func(bg color.Color, w int) string {
		on := 0.0
		if d.AtLogin {
			on = 1
		}
		return ui.L(bg).Right(w, ui.L(bg).Raw(ui.Switch(on, bg)))
	})
	body.Put(2, 5, ui.Paint("Cache", p.Faint, p.Surface, false))
	line(6, eSize, "Size limit", text(d.CacheMaxSize, "No limit"))
	line(7, eAge, "Keep files for", func(bg color.Color, w int) string {
		age := cmp.Or(e.age, d.CacheMaxAge)
		v := ui.L(bg).T(cmp.Or(ageLabels[age], age), p.Text)
		if cur == eAge {
			v = ui.L(bg).T(ui.G.Back+" ", p.Faint).Raw(v.String()).T(" "+ui.G.Arrow, p.Faint)
		}
		return ui.L(bg).Right(w, v)
	})
	win.Body = body
	return 8
}

func (a *Add) editActions(st *core.State) []core.Action {
	e := &a.ed
	if e.field != nil {
		return []core.Action{{Key: "enter", Label: "save"}, {Key: "esc", Label: "cancel"}}
	}
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	switch editRows()[max(0, min(e.focus, len(editRows())-1))] {
	case eLogin:
		label := "turn on"
		if a.live(st).AtLogin {
			label = "turn off"
		}
		acts = append(acts, core.Action{Key: "enter", Label: label})
	case eAge:
		acts = append(acts, core.Action{Key: "← →", Label: "change"})
		if e.age != "" && e.age != a.live(st).CacheMaxAge {
			acts = append(acts, core.Action{Key: "enter", Label: "save", Accent: true})
		}
	default:
		acts = append(acts, core.Action{Key: "enter", Label: "edit"})
	}
	if c := a.connLabel(st); c != "" {
		acts = append(acts, core.Action{Key: "c", Label: c, Accent: c == "connect"})
	}
	if a.canSignIn() {
		acts = append(acts, core.Action{Key: "s", Label: "sign-in"})
	}
	return append(acts, core.Action{Key: "d", Label: "remove", Danger: true}, core.Action{Key: "esc", Label: "close"})
}
