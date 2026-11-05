package add

import (
	"cmp"
	"context"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

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

// a list of values: enter edits one in place and saves it on its own; actions are keys in the footer
type editForm struct {
	focus      int
	field      *ui.Field // the value being typed, nil when only moving
	age        string    // a keep-for picked with the arrows, saved on enter
	req        core.Req
	err        string
	remounting bool

	opened         time.Time // rows fade in once, top to bottom
	saving, saved  int       // the row being saved, and the one that just was
	savedAt        time.Time // its check fades out
	login          *bool     // the switch moves on the press, before the save answers
	loginAt, ageAt time.Time
	ageFrom        string // the keep-for the highlight leaves
}

const (
	revealStep = 35 * time.Millisecond
	revealFor  = 160 * time.Millisecond
	savedFor   = 1600 * time.Millisecond
	slideFor   = 180 * time.Millisecond
)

func (e *editForm) animating(now time.Time) bool {
	return e.req != 0 || now.Sub(e.opened) < 12*revealStep+revealFor || now.Sub(e.savedAt) < savedFor ||
		now.Sub(e.loginAt) < slideFor || now.Sub(e.ageAt) < slideFor
}

// reveal of the k-th element, 0 to 1
func (e *editForm) shown(k int, now time.Time) float64 {
	if ui.Reduced() {
		return 1
	}
	return ui.Ease(0, 1, e.opened.Add(time.Duration(k)*revealStep), now, revealFor)
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
		e.req, e.age, e.remounting, e.login = 0, "", false, nil
		if m.Err != nil {
			e.err = core.Report(m.Err)
			return nil
		}
		e.saved, e.savedAt = e.saving, st.Now
		if renamed, _ := m.Val.(bool); renamed {
			return tea.Batch(core.Say("Saved"), core.Send(core.Recheck{}))
		}
		return core.Say("Saved")
	case core.Click:
		if i, err := strconv.Atoi(strings.TrimPrefix(m.ID, "item:")); err == nil && e.field == nil {
			e.focus = slices.Index(rows, i)
			if i == eLogin {
				return a.flipLogin(st, d)
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
				return a.flipLogin(st, d)
			case row == eAge && (k == "left" || k == "right"):
				ages := engine.CacheMaxAges
				i := slices.Index(ages, cmp.Or(e.age, d.CacheMaxAge))
				if k == "left" {
					i = max(0, i-1)
				} else {
					i = min(len(ages)-1, i+1)
				}
				if ages[i] != cmp.Or(e.age, d.CacheMaxAge) {
					e.ageFrom, e.ageAt = cmp.Or(e.age, d.CacheMaxAge), st.Now
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

func (a *Add) flipLogin(st *core.State, d api.Drive) tea.Cmd {
	on := !d.AtLogin
	if a.ed.login != nil {
		on = !*a.ed.login
	}
	a.ed.login, a.ed.loginAt = &on, st.Now
	return a.saveEdit(st, api.DriveUpdate{AtLogin: &on}, false)
}

func (a *Add) saveEdit(st *core.State, u api.DriveUpdate, renamed bool) tea.Cmd {
	e := &a.ed
	u.ID, e.req, e.err = a.edit.ID, core.NextReq(), ""
	e.saving = editRows()[max(0, min(e.focus, len(editRows())-1))]
	return core.Do(st, e.req, func(ctx context.Context, c *client.Client) (any, error) {
		_, err := c.UpdateDrive(ctx, u)
		return renamed, err
	})
}

// a header with what the drive is and its live state, then two quiet sections of values; the focused row lifts, a saved one ticks
func (a *Add) viewEdit(st *core.State, win *ui.Window) int {
	p := ui.P()
	e := &a.ed
	d := a.live(st)
	cw := win.Inner()
	win.Title = d.Name
	rows := editRows()
	cur := rows[max(0, min(e.focus, len(rows)-1))]
	body := ui.NewBox()
	bg := p.Surface
	fade := func(c color.Color, k int) color.Color { return ui.Mix(bg, c, e.shown(k, st.Now)) }

	head := ui.L(bg)
	switch {
	case e.req != 0:
		what := "Saving"
		if e.remounting {
			what = "Saving and remounting"
		}
		head.B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T(what, p.Muted)
	case e.err != "":
		head.T(core.Fit(e.err, cw-18), p.Coral)
	default:
		head.T(cmp.Or(a.prov.Name, d.ServiceType), fade(p.Muted, 0))
		if d.Account != "" && d.Account != d.Name {
			head.Sp(2).T(d.Account, fade(p.Faint, 0))
		}
		if k := d.AccountDrives; k > 1 {
			head.Sp(2).T("shared with "+core.Count(k-1, "drive"), fade(p.Faint, 0))
		}
	}
	body.Put(0, 0, head.Right(cw, ui.DriveStatus(d, st.T).Line(bg, true)))

	section := func(y, k int, name string) {
		l := ui.L(bg).Sp(2).T(name, fade(p.Faint, k)).Sp(1)
		body.Put(0, y, l.T(strings.Repeat(ui.G.H, max(0, cw-l.Width()-1)), fade(ui.Mix(bg, p.Text, .1), k)).Pad(cw))
	}
	line := func(y, k, row int, label string, value func(bg color.Color, w int, k int) string) {
		body.Add(0, y, ui.Row("item:"+strconv.Itoa(row), cw, row == cur, bg, func(rb color.Color, iw int) string {
			l := ui.L(rb)
			if row == cur {
				l.B(label, p.Text)
			} else {
				l.T(label, fade(ui.Mix(p.Muted, p.Text, .55), k))
			}
			vw := iw - editLabelW
			if row == cur && e.field != nil {
				return ui.L(rb).Raw(l.Pad(editLabelW)).Raw(ui.L(rb).Raw(e.field.View("edit", vw, true, rb).GetContent()).Pad(vw)).String()
			}
			return ui.L(rb).Raw(l.Pad(editLabelW)).Raw(value(rb, vw, k)).String()
		}))
	}
	// a value right aligned; the one just saved shows a check that fades back into it
	text := func(row int, v, empty string) func(rb color.Color, w, k int) string {
		return func(rb color.Color, w, k int) string {
			fg := p.Text
			if v == "" {
				v, fg = empty, p.Faint
			}
			out := ui.L(rb)
			if row == e.saved && !ui.Reduced() {
				if t := float64(st.Now.Sub(e.savedAt)) / float64(savedFor); t < 1 {
					out.B(ui.G.Check+" ", ui.Mix(rb, p.Kelp, 1-t))
					fg = ui.Mix(p.Kelp, fg, t)
				}
			}
			return ui.L(rb).Right(w, out.T(core.Fit(v, w-3), fade(fg, k)))
		}
	}

	section(2, 1, "Drive")
	line(3, 2, eName, "Name", text(eName, d.Name, ""))
	line(4, 3, eSource, "Source folder", text(eSource, d.SubPath, "Whole account"))
	line(5, 4, eMount, "Mount folder", text(eMount, d.LocalFolder, core.Home(d.Folder)))
	line(6, 5, eLogin, "Connect at login", func(rb color.Color, w, k int) string {
		on := d.AtLogin
		if e.login != nil {
			on = *e.login
		}
		frac := 0.0
		if on {
			frac = 1
		}
		if !ui.Reduced() {
			frac = ui.Ease(1-frac, frac, e.loginAt, st.Now, slideFor)
		}
		if e.shown(k, st.Now) < .5 {
			return ui.L(rb).Pad(w)
		}
		return ui.L(rb).Right(w, ui.L(rb).Raw(ui.Switch(frac, rb)))
	})
	section(8, 6, "Cache")
	line(9, 7, eSize, "Size limit", text(eSize, d.CacheMaxSize, "No limit"))
	line(10, 8, eAge, "Keep files for", func(rb color.Color, w, k int) string {
		return ui.L(rb).Right(w, a.ageScale(st, d, rb, cur == eAge, k))
	})
	win.Body = body
	return 11
}

// the keep-for choices on a scale; the highlight crossfades to the next pick
func (a *Add) ageScale(st *core.State, d api.Drive, rb color.Color, focused bool, k int) *ui.Line {
	p := ui.P()
	e := &a.ed
	sel := cmp.Or(e.age, d.CacheMaxAge)
	t := 1.0
	if !ui.Reduced() {
		t = ui.Ease(0, 1, e.ageAt, st.Now, slideFor)
	}
	strength := .22
	if focused {
		strength = .38
	}
	l := ui.L(rb)
	for i, ag := range engine.CacheMaxAges {
		on := 0.0
		switch ag {
		case sel:
			on = t
		case e.ageFrom:
			on = 1 - t
		}
		short := strings.Replace(ag, "24h", "1d", 1)
		fg := ui.Mix(p.Faint, p.Text, on)
		l.Raw(ui.Paint(" "+short+" ", ui.Mix(rb, fg, e.shown(k, st.Now)), ui.Mix(rb, p.Accent, strength*on), on > .5))
		if i < len(engine.CacheMaxAges)-1 {
			l.Sp(1)
		}
	}
	return l
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
