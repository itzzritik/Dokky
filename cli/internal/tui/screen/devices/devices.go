package devices

import (
	"context"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

// the computers on the account, opened from Settings
type Devices struct {
	list   []api.Device
	req    core.Req
	sel    int
	rename *ui.Field
	btn    int
}

func (s *Devices) buttons() []ui.Btn {
	if s.rename != nil {
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "save", Label: "Save", Primary: true}}
	}
	drop := ui.Btn{ID: "remove", Label: "Remove", Danger: true}
	if s.sel < len(s.list) && s.list[s.sel].Current {
		drop = ui.Btn{ID: "signout", Label: "Sign out", Danger: true}
	}
	return []ui.Btn{{ID: "rename", Label: "Rename"}, drop, {ID: "close", Label: "Close", Primary: true}}
}

func Open(st *core.State) tea.Cmd {
	d := &Devices{btn: 2}
	return tea.Sequence(core.Open(d), d.load(st))
}

func (s *Devices) load(st *core.State) tea.Cmd {
	if !st.Snap.Account.LoggedIn || st.Snap.Account.Pending {
		return nil
	}
	s.req = core.NextReq()
	return core.Do(st, s.req, func(ctx context.Context, c *client.Client) (any, error) { return c.Devices(ctx) })
}

func (s *Devices) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case core.Reconnected:
		s.list, s.rename = nil, nil
		return s, s.load(st)
	case core.Reply:
		if m.Req != s.req {
			return s, nil
		}
		s.req = 0
		if m.Err != nil {
			return s, core.Fail(m.Err)
		}
		if list, ok := m.Val.([]api.Device); ok {
			s.list = list
		}
		return s, nil
	case core.Click:
		for i, d := range s.list {
			if m.ID == "row:"+d.ID {
				s.sel = i
			}
		}
		for _, b := range s.buttons() {
			if m.ID == "btn:"+b.ID {
				return s, s.press(b.ID, st)
			}
		}
	case tea.KeyPressMsg:
		return s, s.key(m, st)
	}
	return s, nil
}

func (s *Devices) key(m tea.KeyPressMsg, st *core.State) tea.Cmd {
	k := m.String()
	if k == "esc" && s.rename == nil {
		return core.Close(s)
	}
	if s.rename != nil {
		switch k {
		case "esc":
			return s.press("cancel", st)
		case "enter":
			return s.press("save", st)
		}
		cmd, _ := s.rename.Type(m)
		return cmd
	}
	if len(s.list) == 0 {
		return nil
	}
	s.sel = max(0, min(s.sel, len(s.list)-1))
	d := s.list[s.sel]
	switch k {
	case "up":
		s.sel = max(0, s.sel-1)
	case "down":
		s.sel = min(len(s.list)-1, s.sel+1)
	case "left":
		s.btn = max(0, s.btn-1)
	case "right":
		s.btn = min(len(s.buttons())-1, s.btn+1)
	case "enter":
		return s.press(s.buttons()[s.btn].ID, st)
	case "r":
		return s.press("rename", st)
	case "s":
		if d.Current {
			return s.press("signout", st)
		}
	case "x":
		if !d.Current {
			return s.press("remove", st)
		}
	}
	return nil
}

func (s *Devices) Capturing() bool { return s.rename != nil }

func (s *Devices) press(id string, st *core.State) tea.Cmd {
	if id == "close" {
		return core.Close(s)
	}
	if s.sel >= len(s.list) {
		s.rename = nil
		return nil
	}
	d := s.list[s.sel]
	switch id {
	case "rename":
		s.rename = ui.NewField("", d.Name, "Computer name", false)
		s.rename.Bare = true
	case "cancel":
		s.rename = nil
	case "save":
		name := strings.TrimSpace(s.rename.Value())
		s.rename = nil
		c := st.C
		return tea.Sequence(func() tea.Msg {
			if err := c.RenameDevice(context.Background(), d.ID, name); err != nil {
				return core.Toast{Text: core.Report(err), Bad: true}
			}
			return core.Toast{Text: "Renamed"}
		}, s.load(st))
	case "signout":
		return tea.Batch(core.Close(s), SignOut())
	case "remove":
		return s.remove(d)
	}
	return nil
}

func SignOut() tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Sign out of dokky?",
		Line:     "Drives from your account leave this computer.",
		Do:       "Sign out",
		Run: func(st *core.State, _ bool) tea.Cmd {
			c := st.C
			return func() tea.Msg {
				warn, err := c.Logout(context.Background(), false)
				if err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return core.Toast{Text: core.Safe(cmpOr(warn, "Signed out"))}
			}
		},
	})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Devices) remove(d api.Device) tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Remove " + core.Safe(d.Name) + "?",
		Line:     "It signs out and stops getting your drives.",
		Do:       "Remove",
		Run: func(st *core.State, _ bool) tea.Cmd {
			c, id := st.C, d.ID
			return tea.Sequence(func() tea.Msg {
				if err := c.RevokeDevice(context.Background(), id); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return core.Toast{Text: "Removed"}
			}, s.load(st))
		},
	})
}

func (s *Devices) Actions(st *core.State) []core.Action {
	switch {
	case s.rename != nil:
		return []core.Action{{Key: "enter", Label: "save"}, {Key: "esc", Label: "cancel"}}
	case len(s.list) == 0:
		return []core.Action{{Key: "esc", Label: "close"}}
	}
	return []core.Action{{Key: "↑↓", Label: "computer"}, {Key: "← →", Label: "button"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "close"}}
}

func (s *Devices) Animating(*core.State) bool { return s.req != 0 && s.list == nil && !ui.Reduced() }

func osName(o string) string {
	switch o {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return o
}

func (s *Devices) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(72, maxW), Title: "Your computers", Sub: "Your drives sync to all of these.", Buttons: s.buttons(), Active: s.btn}
	if s.rename != nil {
		win.Active = 1
	}
	cw := win.Inner()
	body := ui.NewBox()
	rows := 1
	if s.list == nil {
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Loading", p.Muted).Pad(cw))
	} else {
		s.sel = max(0, min(s.sel, len(s.list)-1))
		fit := max(1, (win.Room(maxH)+1)/3)
		top := max(0, s.sel-fit+1)
		for i := top; i < len(s.list) && i < top+fit; i++ {
			y := (i - top) * 3
			s.entry(body, y, st, s.list[i], cw, i == s.sel)
			rows = y + 2
		}
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win.Layer()
}

// one computer in two lines: name and when it was seen, then what it runs
func (s *Devices) entry(b *ui.Box, y int, st *core.State, d api.Device, w int, on bool) {
	p := ui.P()
	first := ui.RowLine(w, on, p.Surface, func(bg color.Color, iw int) string {
		if on && s.rename != nil {
			return ui.L(bg).Raw(s.rename.View("rename", iw, true, bg).GetContent()).Pad(iw)
		}
		name := ui.L(bg)
		if on {
			name.B(d.Name, p.Text)
		} else {
			name.T(d.Name, p.Text)
		}
		if d.Current {
			name.Sp(2).T("this computer", p.Accent)
		}
		seen := ui.L(bg).T(core.Ago(d.LastSeenAt, st.Now), p.Faint)
		if d.Current {
			seen = ui.L(bg).T("Active now", p.Kelp)
		}
		return name.Right(iw, seen)
	})
	second := ui.RowLine(w, on, p.Surface, func(bg color.Color, iw int) string {
		return ui.L(bg).T(osName(d.OS)+"  ·  "+core.Count(d.Drives, "drive"), p.Muted).Pad(iw)
	})
	b.Hit(0, y, "row:"+d.ID, first)
	b.Hit(0, y+1, "row:"+d.ID, second)
}
