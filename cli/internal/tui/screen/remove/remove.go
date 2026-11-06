package remove

import (
	"context"
	"fmt"
	"image/color"
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
	active int
}

// parent, when set, closes too once the drive is removed
func Open(st *core.State, d api.Drive, parent core.Screen) tea.Cmd {
	r := &Remove{d: d, parent: parent}
	return tea.Sequence(core.Open(r), r.load(st))
}

func (r *Remove) load(st *core.State) tea.Cmd {
	r.req, r.check, r.err = core.NextReq(), nil, ""
	id := r.d.ID
	return core.Do(st, r.req, func(ctx context.Context, c *client.Client) (any, error) { return c.CheckDrive(ctx, id) })
}

func (r *Remove) busy() bool      { return r.check != nil && r.check.Usage.State == "busy" }
func (r *Remove) stuck() bool     { return r.check != nil && r.check.Usage.State == "stuck" }
func (r *Remove) elsewhere() bool { return r.check != nil && len(r.check.Others) > 0 }

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
		switch m.String() {
		case "esc":
			return r, core.Close(r)
		case "left", "right":
			if n := len(r.buttons()); n > 1 {
				r.active = (r.active + 1) % n
			}
		case "space":
			r.toggle()
		case "enter":
			return r, r.press(st, r.active)
		}
	case core.Click:
		switch m.ID {
		case "btn:cancel":
			return r, r.press(st, 0)
		case "btn:do":
			return r, r.press(st, 1)
		case "chk:sure":
			r.toggle()
		}
	}
	return r, nil
}

func (r *Remove) toggle() {
	if r.elsewhere() {
		r.sure, r.nudge = !r.sure, false
	}
}

func (r *Remove) press(st *core.State, i int) tea.Cmd {
	if i == 0 || i >= len(r.buttons()) {
		return core.Close(r)
	}
	if r.elsewhere() && !r.sure {
		r.nudge = true
		return nil
	}
	c, id, name, anyway := st.C, r.d.ID, core.Safe(r.d.Name), r.stuck()
	cmds := []tea.Cmd{core.Close(r)}
	if r.parent != nil {
		cmds = append(cmds, core.Close(r.parent))
	}
	return tea.Batch(append(cmds, func() tea.Msg {
		if err := c.RemoveDrive(context.Background(), api.DriveRemove{ID: id, Anyway: anyway}); err != nil {
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

func (r *Remove) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(64, maxW), Title: "Remove " + core.Safe(r.d.Name) + "?", Buttons: r.buttons(), Active: min(r.active, len(r.buttons())-1)}
	cw := win.Inner()
	body := ui.NewBox()
	y := 0
	line := func(s string, fg color.Color) {
		body.Put(0, y, ui.L(p.Surface).T(core.Fit(s, cw), fg).Pad(cw))
		y++
	}
	switch {
	case r.err != "":
		win.Sub, win.SubBad = "Couldn't check this drive", true
		line(r.err, p.Muted)
	case r.check == nil:
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Checking where it's mounted", p.Muted).Pad(cw))
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
	if r.elsewhere() && !r.busy() {
		y++
		frac := 0.0
		if r.sure {
			frac = 1
		}
		row := ui.L(p.Surface).Raw(ui.Switch(frac, p.Surface)).Sp(1).T("I understand", p.Text).Pad(cw)
		body.Add(0, y, lipgloss.NewLayer(row).ID("chk:sure"))
		y++
	}
	win.Fit(y, maxH)
	if y > 0 {
		win.Body = body
	}
	return win.Layer()
}

func (r *Remove) Actions(*core.State) []core.Action {
	acts := []core.Action{{Key: "← →", Label: "choose"}}
	if r.elsewhere() && !r.busy() {
		acts = append(acts, core.Action{Key: "space", Label: "I understand"})
	}
	return append(acts, core.Action{Key: "enter", Label: "select"}, core.Action{Key: "esc", Label: "cancel"})
}

func (r *Remove) Animating(*core.State) bool { return r.check == nil && r.err == "" && !ui.Reduced() }
