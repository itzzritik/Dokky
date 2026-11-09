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
	"github.com/itzzritik/dokky/cli/internal/tui/screen/remove"
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

// a list of values: enter edits one in place and saves it on its own; the drive's actions are buttons under it
type editForm struct {
	at         string    // the focused row or button
	field      *ui.Field // the value being typed, nil when only moving
	req        core.Req
	err        string
	remounting bool

	saving, saved int       // the row being saved, and the one that just was
	savedAt       time.Time // its check fades out
	login         *bool     // the switch moves on the press, before the save answers
	loginAt       time.Time
	menu          bool // the keep-for list is open under its row
	menuSel       int
	menuAt        time.Time
}

const (
	revealStep = 35 * time.Millisecond
	revealFor  = 160 * time.Millisecond
	savedFor   = 1600 * time.Millisecond
	slideFor   = 180 * time.Millisecond
)

func (e *editForm) animating(now time.Time) bool {
	return e.req != 0 || now.Sub(e.savedAt) < savedFor ||
		now.Sub(e.loginAt) < slideFor || e.menu && now.Sub(e.menuAt) < 8*revealStep+revealFor
}

func editRows() []int { return []int{eName, eSource, eMount, eLogin, eSize, eAge} }

func rowID(row int) string { return "item:" + strconv.Itoa(row) }

func editStops() []string {
	var out []string
	for _, r := range editRows() {
		out = append(out, rowID(r))
	}
	return out
}

// which row a stop is, -1 for a button
func rowAt(at string) int {
	if r, ok := strings.CutPrefix(at, "item:"); ok {
		if i, err := strconv.Atoi(r); err == nil {
			return i
		}
	}
	return -1
}

// letters that press the bar's buttons
var editKeys = map[string]string{"m": "mount", "v": "view", "s": "signin", "d": "remove"}

// a click or button outside drops the open list or the value being typed
func (e *editForm) close() { e.menu, e.field = false, nil }

// the drive as the daemon has it now, not as the window opened
func (a *Add) live(st *core.State) api.Drive {
	if d, ok := st.Drive(a.edit.ID); ok {
		return d
	}
	return *a.edit
}

// browser sign-ins are only redone when the login failed
func (a *Add) canSignIn(st *core.State) bool {
	return a.edit.ServiceType != "" && (!oauthType(a.edit.ServiceType) || ui.LoginFailed(a.live(st)))
}

func (a *Add) updateEdit(msg tea.Msg, st *core.State) tea.Cmd {
	e := &a.ed
	d := a.live(st)
	switch m := msg.(type) {
	case core.Reply:
		if m.Req != e.req {
			return nil
		}
		e.req, e.remounting, e.login = 0, false, nil
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
		return a.clickEdit(m.ID, st, d)
	case tea.KeyPressMsg:
		if e.req != 0 {
			return nil
		}
		return a.keyEdit(m.String(), m, st, d)
	}
	return nil
}

// a click closes what is open first, then lands: the login switch flips, keep-for opens, a focused value starts typing
func (a *Add) clickEdit(id string, st *core.State, d api.Drive) tea.Cmd {
	e := &a.ed
	if i, err := strconv.Atoi(strings.TrimPrefix(id, "opt:")); err == nil && e.menu {
		e.menuSel = i
		return a.pickAge(st, d)
	}
	row := rowAt(id)
	if row < 0 || e.req != 0 {
		return nil
	}
	was, menu := rowAt(e.at), e.menu
	// a click inside the value being typed keeps typing
	if e.field != nil && row == was {
		return nil
	}
	typing := e.field != nil
	e.close()
	e.at = id
	switch {
	case row == eLogin:
		return a.flipLogin(st, d)
	case row == eAge && !menu:
		a.openMenu(st, d)
	case row == was && row != eAge && !typing:
		a.startEdit(row, d)
	}
	return nil
}

func (a *Add) keyEdit(k string, m tea.KeyPressMsg, st *core.State, d api.Drive) tea.Cmd {
	e := &a.ed
	if e.menu {
		switch k {
		case "esc":
			e.menu = false
		case "up":
			e.menuSel = max(0, e.menuSel-1)
		case "down":
			e.menuSel = min(len(engine.CacheMaxAges)-1, e.menuSel+1)
		case "enter", "space":
			return a.pickAge(st, d)
		}
		return nil
	}
	if e.field != nil {
		switch k {
		case "esc":
			e.field = nil
			return nil
		case "enter":
			return a.commit(st, rowAt(e.at), d)
		}
		cmd, typed := e.field.Type(m)
		if typed {
			e.err = ""
		}
		return cmd
	}
	e.at = a.focused(st)
	row := rowAt(e.at)
	switch {
	case ui.Pressed(k, e.at):
		return a.press(strings.TrimPrefix(e.at, "btn:"), st)
	// right turns it on and left turns it off, whichever way it is now
	case row == eLogin && (k == "left" || k == "right"):
		return a.setLogin(st, d, k == "right")
	case row == eLogin && (k == "enter" || k == "space"):
		return a.flipLogin(st, d)
	case row == eAge && (k == "enter" || k == "space"):
		a.openMenu(st, d)
	case row >= 0 && k == "enter":
		a.startEdit(row, d)
	case ui.Walk(a.stops(st), &e.at, k):
	case editKeys[k] != "" && slices.Contains(a.stops(st), "btn:"+editKeys[k]):
		e.at = "btn:" + editKeys[k]
		return a.press(editKeys[k], st)
	}
	return nil
}

func (a *Add) openMenu(st *core.State, d api.Drive) {
	e := &a.ed
	e.menu, e.menuAt = true, st.Now
	e.menuSel = max(0, slices.Index(engine.CacheMaxAges, d.CacheMaxAge))
}

func (a *Add) startEdit(row int, d api.Drive) {
	e := &a.ed
	e.err = ""
	e.field = ui.NewField("", a.editValue(row, d), a.editHint(row, d), false)
	e.field.Bare = true
}

// Remove apart on the left, then sign in, view and mount, the drive's own state naming the last
func (a *Add) editBar(st *core.State) []ui.Btn {
	d := a.live(st)
	login := ui.LoginFailed(d)
	out := []ui.Btn{{ID: "remove", Label: "Remove", Danger: true}}
	if a.canSignIn(st) {
		out = append(out, ui.Btn{ID: "signin", Label: "Sign in", Primary: login})
	}
	if d.State == api.Connected {
		out = append(out, ui.Btn{ID: "view", Label: upperFirst(core.ViewLabel())})
	}
	// the mount button stays while it works, so focus never jumps off it
	m := core.MountLabel(d)
	mount := ui.Btn{ID: "mount", Label: ui.Spin(st.T) + " " + ui.DriveStatus(d, st.T).Words, Primary: m != "unmount" && !login}
	if m != "" {
		mount.Label = upperFirst(m)
	}
	return append(out, mount)
}

func (a *Add) editPress(id string, st *core.State) tea.Cmd {
	if a.edit == nil {
		return nil
	}
	d := a.live(st)
	switch {
	// the window stays open; its corner shows the drive mounting
	case id == "mount" && core.MountLabel(d) != "":
		return core.RunMount(st, d)
	case id == "view" && d.State == api.Connected:
		return core.OpenFolder(d.Folder)
	case id == "signin" && a.canSignIn(st):
		return a.toSignIn(st)
	case id == "remove":
		return remove.Open(st, *a.edit, a)
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
		Line:     name + " unmounts for a moment to remount.",
		Do:       "Save",
		Positive: true,
		Run: func(st *core.State) tea.Cmd {
			a.ed.remounting = true
			return a.saveEdit(st, u, renamed)
		},
	})
}

func (a *Add) pickAge(st *core.State, d api.Drive) tea.Cmd {
	a.ed.menu = false
	if ag := engine.CacheMaxAges[a.ed.menuSel]; ag != d.CacheMaxAge {
		return a.remountSave(st, d, api.DriveUpdate{CacheMaxAge: ag}, false)
	}
	return nil
}

// the switch as it shows, ahead of the save's answer
func (a *Add) loginOn(d api.Drive) bool {
	if a.ed.login != nil {
		return *a.ed.login
	}
	return d.AtLogin
}

func (a *Add) flipLogin(st *core.State, d api.Drive) tea.Cmd {
	on := !a.loginOn(d)
	a.ed.login, a.ed.loginAt = &on, st.Now
	return a.saveEdit(st, api.DriveUpdate{AtLogin: &on}, false)
}

func (a *Add) setLogin(st *core.State, d api.Drive, on bool) tea.Cmd {
	if a.loginOn(d) == on {
		return nil
	}
	return a.flipLogin(st, d)
}

func (a *Add) saveEdit(st *core.State, u api.DriveUpdate, renamed bool) tea.Cmd {
	e := &a.ed
	u.ID, e.req, e.err = a.edit.ID, core.NextReq(), ""
	e.saving = rowAt(e.at)
	return core.Do(st, e.req, func(ctx context.Context, c *client.Client) (any, error) {
		_, err := c.UpdateDrive(ctx, u)
		return renamed, err
	})
}

var ageLabels = map[string]string{"5m": "5 minutes", "15m": "15 minutes", "1h": "1 hour", "6h": "6 hours", "24h": "1 day", "7d": "7 days"}

// the live status in the top border, two quiet sections of values in one column; a saved one ticks, keep-for opens a list
func (a *Add) viewEdit(st *core.State, win *ui.Window, at string) int {
	p := ui.P()
	e := &a.ed
	d := a.live(st)
	cw := win.Inner()
	bg := p.Surface
	win.Title = d.Name
	switch {
	case e.req != 0:
		what := "Saving"
		if e.remounting {
			what = "Saving and remounting"
		}
		win.Corner = ui.L(bg).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T(what, p.Muted).String()
	default:
		win.Corner = ui.DriveStatus(d, st.T).Line(bg, true).String()
	}
	if e.err != "" {
		win.Sub, win.SubBad = e.err, true
	}
	cur := rowAt(at)
	body := ui.NewBox()
	section := func(y int, name string) {
		l := ui.L(bg).Sp(2).T(name, p.Faint).Sp(1)
		body.Put(0, y, l.T(strings.Repeat(ui.G.H, max(0, cw-l.Width()-1)), ui.Mix(bg, p.Text, .1)).Pad(cw))
	}
	line := func(y, row int, label string, value func(rb color.Color, w int) string) {
		body.Add(0, y, ui.Row("item:"+strconv.Itoa(row), cw, row == cur, bg, func(rb color.Color, iw int) string {
			l := ui.L(rb)
			if row == cur {
				l.B(label, p.Text)
			} else {
				l.T(label, ui.Mix(p.Muted, p.Text, .55))
			}
			vw := iw - editLabelW
			v := value(rb, vw)
			if row == cur && e.field != nil {
				v = e.field.View("edit", vw, true, rb).GetContent()
			}
			return ui.L(rb).Raw(l.Pad(editLabelW)).Raw(ui.L(rb).Raw(v).Pad(vw)).String()
		}))
	}
	// values start where an edit starts, so editing never moves them; a saved one shows a check that fades out
	text := func(row int, v, empty string) func(rb color.Color, w int) string {
		return func(rb color.Color, w int) string {
			fg := p.Text
			if v == "" {
				v, fg = empty, p.Faint
			}
			out := ui.L(rb).T(core.Fit(v, w-3), fg)
			if row == e.saved && !ui.Reduced() {
				if t := float64(st.Now.Sub(e.savedAt)) / float64(savedFor); t < 1 {
					out.Sp(2).B(ui.G.Check, ui.Mix(rb, p.Kelp, 1-t))
				}
			}
			return out.String()
		}
	}

	section(0, "Drive")
	line(1, eName, "Name", text(eName, d.Name, ""))
	service := ui.L(bg).Sp(2).Raw(ui.L(bg).T("Service", ui.Mix(p.Muted, p.Text, .55)).Pad(editLabelW)).T(cmp.Or(a.prov.Name, d.ServiceType), p.Text)
	if d.Account != "" && d.Account != d.Name {
		service.Sp(2).T(d.Account, p.Faint)
	}
	if k := d.AccountDrives; k > 1 {
		service.Sp(2).T("shared with "+core.Count(k-1, "drive"), p.Faint)
	}
	body.Put(0, 2, service.Pad(cw))
	line(3, eSource, "Source folder", text(eSource, d.SubPath, "Whole account"))
	line(4, eMount, "Mount folder", text(eMount, d.LocalFolder, core.Home(d.Folder)))
	line(5, eLogin, "Mount at login", func(rb color.Color, w int) string {
		on := a.loginOn(d)
		frac := 0.0
		if on {
			frac = 1
		}
		if !ui.Reduced() {
			frac = ui.Ease(1-frac, frac, e.loginAt, st.Now, slideFor)
		}
		return ui.Switch(frac, rb)
	})
	section(7, "Cache")
	line(8, eSize, "Size limit", text(eSize, d.CacheMaxSize, "No limit"))
	line(9, eAge, "Keep files for", func(rb color.Color, w int) string {
		caret := ui.G.Caret
		if e.menu {
			caret = "▴"
			if ui.G.ASCII {
				caret = "^"
			}
		}
		return ui.L(rb).T(cmp.Or(ageLabels[d.CacheMaxAge], d.CacheMaxAge), p.Text).Sp(2).T(caret, p.Faint).String()
	})
	rowsUsed := 10
	if e.menu {
		// the list drops open under its row, items fading in one after another
		for i, ag := range engine.CacheMaxAges {
			t := ui.Ease(0, 1, e.menuAt.Add(time.Duration(i)*revealStep), st.Now, revealFor)
			if ui.Reduced() {
				t = 1
			}
			on := i == e.menuSel
			ib := bg
			if on {
				ib = ui.Mix(bg, p.Accent, .22)
			}
			mark, mc := " ", p.Faint
			if ag == d.CacheMaxAge {
				mark, mc = ui.G.Check, p.Kelp
			}
			item := ui.L(ib).B(mark, ui.Mix(ib, mc, t)).Sp(1).T(ageLabels[ag], ui.Mix(ib, ui.Mix(p.Muted, p.Text, map[bool]float64{true: 1, false: .5}[on]), t)).Pad(16)
			body.Hit(2+editLabelW, 10+i, "opt:"+strconv.Itoa(i), item)
		}
		rowsUsed += len(engine.CacheMaxAges)
	}
	win.Body = body
	return rowsUsed
}

func (a *Add) editActions(st *core.State, at string) []core.Action {
	e := &a.ed
	if e.field != nil {
		return []core.Action{{Key: "enter", Label: "save"}, {Key: "esc", Label: "cancel"}}
	}
	if e.menu {
		return []core.Action{{Key: "↑↓", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "close"}}
	}
	esc := core.Action{Key: "esc", Label: "close"}
	if ui.IsBtn(at) {
		return append(a.pressing(st, at), esc)
	}
	var acts []core.Action
	switch rowAt(at) {
	case eLogin:
		label := "turn on"
		if a.loginOn(a.live(st)) {
			label = "turn off"
		}
		acts = []core.Action{{Key: "enter", Label: label}, {Key: "← →", Label: "off/on"}}
	case eAge:
		acts = []core.Action{{Key: "enter", Label: "change"}}
	default:
		acts = []core.Action{{Key: "enter", Label: "edit"}}
	}
	return append(acts, moveAction, esc)
}
