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
}

func Open(st *core.State) tea.Cmd {
	d := &Devices{}
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
			s.rename = nil
		case "enter":
			if s.sel >= len(s.list) {
				s.rename = nil
				return nil
			}
			name, id := strings.TrimSpace(s.rename.Value()), s.list[s.sel].ID
			s.rename = nil
			c := st.C
			return tea.Sequence(func() tea.Msg {
				if err := c.RenameDevice(context.Background(), id, name); err != nil {
					return core.Toast{Text: core.Safe(err.Error()), Bad: true}
				}
				return core.Toast{Text: "Renamed"}
			}, s.load(st))
		default:
			var cmd tea.Cmd
			s.rename.Input, cmd = s.rename.Input.Update(m)
			return cmd
		}
		return nil
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
	case "r":
		s.rename = ui.NewField("", d.Name, "Computer name", false)
	case "s":
		if d.Current {
			return tea.Batch(core.Close(s), SignOut())
		}
	case "x":
		if !d.Current {
			return s.remove(d)
		}
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
					return core.Toast{Text: core.Safe(err.Error()), Bad: true}
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
					return core.Toast{Text: core.Safe(err.Error()), Bad: true}
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
	acts := []core.Action{{Key: "↑↓", Label: "move"}}
	if s.list[min(s.sel, len(s.list)-1)].Current {
		acts = append(acts, core.Action{Key: "s", Label: "sign out", Danger: true})
	} else {
		acts = append(acts, core.Action{Key: "x", Label: "remove", Danger: true})
	}
	return append(acts, core.Action{Key: "r", Label: "rename"}, core.Action{Key: "esc", Label: "close"})
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
	win := &ui.Window{W: min(84, maxW), Title: "Your computers", Sub: "Your drives sync to all of these."}
	cw := win.Inner()
	body := ui.NewBox()
	rows := 1
	switch {
	case s.list == nil:
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), p.Accent).Sp(1).T("Loading", p.Muted).Pad(cw))
	default:
		s.sel = max(0, min(s.sel, len(s.list)-1))
		room := max(1, maxH-7-2)
		top := max(0, s.sel-room+1)
		for i := top; i < len(s.list) && i < top+room; i++ {
			d, on, y := s.list[i], i == s.sel, i-top
			if on && s.rename != nil {
				body.Add(0, y, s.rename.View("rename", cw, true, p.Surface))
				continue
			}
			body.Add(0, y, ui.Row("row:"+d.ID, cw, on, p.Surface, func(bg color.Color, iw int) string {
				name := ui.L(bg)
				if on {
					name.B(d.Name, p.Text)
				} else {
					name.T(d.Name, p.Text)
				}
				if d.Current {
					name.Sp(2).T("this computer", p.Accent)
				}
				col := max(20, iw-10-10-12)
				l := ui.L(bg).Raw(name.Pad(col)).Raw(ui.L(bg).T(osName(d.OS), p.Muted).Pad(10)).T(core.Count(d.Drives, "drive"), p.Muted)
				seen := ui.L(bg).T(core.Ago(d.LastSeenAt, st.Now), p.Faint)
				if d.Current {
					seen = ui.L(bg).T("Active now", p.Kelp)
				}
				return l.Right(iw, seen)
			}))
			rows = y + 1
		}
	}
	win.H = min(maxH, 7+rows+2)
	win.Body = body
	return win.Layer()
}
