package remove

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// asks where else the drive is mounted before removing it from every device
type Remove struct {
	d      api.Drive
	parent core.Screen
	req    core.Req
	check  *api.DriveCheck
	err    string
	sure   bool
	nudge  bool
	at     string // the focused stop
}

const sureID = "chk:sure"

// parent, when set, closes too once the drive is removed
func Open(st *core.State, d api.Drive, parent core.Screen) tea.Cmd {
	r := newRemove(d, parent)
	return tea.Sequence(core.Open(r), r.load(st))
}

// Cancel has focus first, so a stray enter never removes anything
func newRemove(d api.Drive, parent core.Screen) *Remove {
	return &Remove{d: d, parent: parent, at: "btn:cancel"}
}

func (r *Remove) load(st *core.State) tea.Cmd {
	r.req, r.check, r.err = core.NextReq(), nil, ""
	id := r.d.ID
	return core.Do(st, r.req, func(ctx context.Context, c *client.Client) (any, error) { return c.CheckDrive(ctx, id) })
}

func (r *Remove) busy() bool      { return r.check != nil && r.check.Usage.State == "busy" }
func (r *Remove) stuck() bool     { return r.check != nil && r.check.Usage.State == "stuck" }
func (r *Remove) elsewhere() bool { return r.check != nil && len(r.check.Others) > 0 }

// the bar, also listed by the actions menu
func (r *Remove) Buttons(*core.State) []ui.Btn { return r.buttons() }

func (r *Remove) buttons() []ui.Btn {
	btns := []ui.Btn{{ID: "cancel", Label: "Cancel"}}
	if r.check == nil || r.busy() {
		return btns
	}
	label := "Remove"
	if r.elsewhere() {
		label = "Remove anyway"
	}
	return append(btns, ui.Btn{ID: "do", Label: label, Danger: true})
}

// the I understand switch shows once the drive is mounted elsewhere
func (r *Remove) asks() bool { return r.elsewhere() && !r.busy() }

// the switch, then the buttons along the bar
func (r *Remove) stops() []string {
	var stops []string
	if r.asks() {
		stops = append(stops, sureID)
	}
	for _, b := range r.buttons() {
		stops = append(stops, "btn:"+b.ID)
	}
	return stops
}

func (r *Remove) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case core.Reply:
		if m.Req != r.req {
			return r, nil
		}
		r.req = 0
		if m.Err != nil {
			r.err = core.Report(m.Err)
			return r, nil
		}
		if c, ok := m.Val.(api.DriveCheck); ok {
			r.check = &c
		}
	case tea.KeyPressMsg:
		k := m.String()
		// a stop that went away, like the switch once the drive is busy, hands focus back to Cancel
		if !slices.Contains(r.stops(), r.at) {
			r.at = "btn:cancel"
		}
		switch {
		case k == "esc":
			return r, core.Close(r)
		case r.at == sureID && (k == "enter" || k == "space"):
			r.toggle()
		case ui.Pressed(k, r.at):
			return r, r.press(st, r.at)
		case ui.Walk(r.stops(), &r.at, k):
		}
	case core.Click:
		if !slices.Contains(r.stops(), m.ID) {
			return r, nil
		}
		r.at = m.ID
		if m.ID == sureID {
			r.toggle()
			return r, nil
		}
		return r, r.press(st, m.ID)
	}
	return r, nil
}

func (r *Remove) toggle() {
	if r.elsewhere() {
		r.sure, r.nudge = !r.sure, false
	}
}

func (r *Remove) press(st *core.State, id string) tea.Cmd {
	if id != "btn:do" || len(r.buttons()) < 2 {
		return core.Close(r)
	}
	if r.elsewhere() && !r.sure {
		r.nudge = true
		return nil
	}
	c, did, name, anyway := st.C, r.d.ID, core.Safe(r.d.Name), r.stuck()
	cmds := []tea.Cmd{core.Close(r)}
	if r.parent != nil {
		cmds = append(cmds, core.Close(r.parent))
	}
	return tea.Batch(append(cmds, func() tea.Msg {
		if err := c.RemoveDrive(context.Background(), api.DriveRemove{ID: did, Anyway: anyway}); err != nil {
			return core.Toast{Text: core.Report(err), Bad: true}
		}
		return core.Toast{Text: name + " removed"}
	})...)
}

func devices(n int) string {
	if n == 1 {
		return "1 other device"
	}
	return fmt.Sprintf("%d other devices", n)
}

func (r *Remove) window(st *core.State, maxW, maxH int) *ui.Window {
	p := ui.P()
	// focus on the switch lights no button, so only one thing looks focused
	win := &ui.Window{W: min(64, maxW), Title: "Remove " + core.Safe(r.d.Name) + "?", Buttons: r.buttons(), Active: -1, Hints: ui.WindowKeys(r.Actions(st))}
	if r.at != sureID || !r.asks() {
		win.Active = max(0, win.Index(r.at))
	}
	cw := win.Inner()
	body := ui.NewBox()
	y := 0
	line := func(s string, fg color.Color) {
		body.Put(0, y, ui.L(p.Window).T(core.Fit(s, cw), fg).Pad(cw))
		y++
	}
	switch {
	case r.err != "":
		win.Sub, win.SubBad = "Couldn't check this drive", true
		line(r.err, p.Muted)
	case r.check == nil:
		body.Put(0, 0, ui.L(p.Window).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Checking where it's mounted", p.Muted).Pad(cw))
		y = 1
	case r.busy():
		win.Sub, win.SubBad = "In use on this device", true
		line(r.check.Usage.Reason+". Try again when it's done.", p.Muted)
	case r.elsewhere():
		win.Sub = "It's removed from every device."
		if r.nudge {
			win.Sub, win.SubBad = "Tick I understand first.", true
		}
		var names []string
		for _, o := range r.check.Others {
			n := o.Name
			if !o.Active {
				n += " (offline)"
			}
			names = append(names, strings.TrimSuffix(n, ".local"))
		}
		line("Mounted on "+devices(len(names))+": "+strings.Join(names, ", "), p.Text)
	default:
		win.Sub = "Its folder and cache leave this device. Your files stay."
	}
	if r.stuck() {
		line(r.check.Usage.Reason+". They'll be dropped.", p.Buoy)
	}
	if r.asks() {
		y++
		body.Add(0, y, r.sureRow(cw))
		y++
	}
	win.Fit(y, maxH)
	if y > 0 {
		win.Body = body
	}
	return win
}

func (r *Remove) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return r.window(st, maxW, maxH).Layer()
}

// the I understand switch as a row, with the list's selection look while it has focus
func (r *Remove) sureRow(w int) *lipgloss.Layer {
	p := ui.P()
	focused := r.at == sureID
	frac := 0.0
	if r.sure {
		frac = 1
	}
	return ui.Row(sureID, w, focused, p.Window, func(bg color.Color, iw int) string {
		l := ui.L(bg).Raw(ui.Switch(frac, bg)).Sp(1)
		if focused {
			return l.B("I understand", p.Text).Pad(iw)
		}
		return l.T("I understand", p.Text).Pad(iw)
	})
}

// enter names what it does on the focused stop; ← → shows only on a bar of two
func (r *Remove) Actions(*core.State) []core.Action {
	enter := core.Action{Key: "enter", Label: "cancel"}
	switch {
	case r.at == sureID && r.asks():
		enter.Label = "tick"
		if r.sure {
			enter.Label = "untick"
		}
	case r.at == "btn:do" && len(r.buttons()) > 1:
		enter = core.Action{Key: "enter", Label: strings.ToLower(r.buttons()[1].Label), Danger: true}
	}
	acts := []core.Action{enter}
	if r.asks() {
		acts = append(acts, core.Action{Key: "↑↓", Label: "move"})
	}
	if ui.IsBtn(r.at) && len(r.buttons()) > 1 {
		acts = append(acts, core.Action{Key: "← →", Label: "choose"})
	}
	return append(acts, core.Action{Key: "esc", Label: "cancel"})
}

func (r *Remove) Animating(*core.State) bool { return r.check == nil && r.err == "" && !ui.Reduced() }
