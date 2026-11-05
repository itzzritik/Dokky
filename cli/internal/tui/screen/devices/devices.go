package devices

import (
	"cmp"
	"context"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/confirm"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

const (
	sysW      = 18
	seenW     = 13
	activeFor = 10 * time.Minute
)

// the devices on the account as a table, opened from Settings
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
	case core.Click:
		for i, d := range s.list {
			if m.ID == "row:"+d.ID && s.rename == nil {
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
	if s.rename != nil {
		switch k {
		case "esc":
			s.rename = nil
			return nil
		case "enter":
			return s.save(st)
		}
		cmd, _ := s.rename.Type(m)
		return cmd
	}
	if k == "esc" {
		return core.Close(s)
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
		s.rename = ui.NewField("", d.Name, "Device name", false)
		s.rename.Bare = true
	case "d":
		if d.Current {
			return tea.Batch(core.Close(s), SignOut())
		}
		return s.remove(d)
	}
	return nil
}

func (s *Devices) Capturing() bool { return s.rename != nil }

func (s *Devices) save(st *core.State) tea.Cmd {
	name := strings.TrimSpace(s.rename.Value())
	s.rename = nil
	if s.sel >= len(s.list) || name == "" || name == s.list[s.sel].Name {
		return nil
	}
	c, id := st.C, s.list[s.sel].ID
	return tea.Sequence(func() tea.Msg {
		if err := c.RenameDevice(context.Background(), id, name); err != nil {
			return core.Toast{Text: core.Report(err), Bad: true}
		}
		return core.Toast{Text: "Renamed"}
	}, s.load(st))
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
				return core.Toast{Text: core.Safe(cmp.Or(warn, "Signed out"))}
			}
		},
	})
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
	var acts []core.Action
	if len(s.list) > 1 {
		acts = append(acts, core.Action{Key: "↑↓", Label: "move"})
	}
	return append(acts, core.Action{Key: "r", Label: "rename"}, core.Action{Key: "d", Label: "delete", Danger: true}, core.Action{Key: "esc", Label: "close"})
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

// the name column takes what the system and last active columns leave; the system goes first on a narrow screen
func columns(iw int) (nw, sw int) {
	sw = sysW
	if nw = iw - 2 - 2 - sw - 2 - seenW; nw < 14 {
		sw, nw = 0, iw-2-2-seenW
	}
	return max(1, nw), sw
}

func (s *Devices) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(84, maxW), Title: "Your devices"}
	cw := win.Inner()
	iw := cw - 3
	nw, sw := columns(iw)
	body := ui.NewBox()
	head := ui.L(p.Surface).Sp(2).Raw(ui.L(p.Surface).T("Device", p.Faint).Pad(nw - 2))
	if sw > 0 {
		head.Sp(2).Raw(ui.L(p.Surface).T("System", p.Faint).Pad(sw))
	}
	body.Put(0, 0, ui.L(p.Surface).Sp(2).Raw(head.Right(iw, ui.L(p.Surface).T("Last active", p.Faint))).Pad(cw))
	body.Put(0, 1, ui.L(p.Surface).Sp(2).T(strings.Repeat(ui.G.H, iw), p.Line).Pad(cw))
	rows := 3
	switch {
	case !st.Snap.Account.LoggedIn:
		body.Put(0, 2, ui.L(p.Surface).Sp(2).T("Sign in to see your devices.", p.Muted).Pad(cw))
	case s.list == nil:
		body.Put(0, 2, ui.L(p.Surface).Sp(2).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Loading", p.Muted).Pad(cw))
	default:
		s.sel = max(0, min(s.sel, len(s.list)-1))
		fit := max(1, win.Room(maxH)-2)
		top := max(0, s.sel-fit+1)
		for i := top; i < len(s.list) && i < top+fit; i++ {
			body.Hit(0, 2+i-top, "row:"+s.list[i].ID, s.entry(st, s.list[i], cw, nw, sw, i == s.sel))
			rows = 3 + i - top
		}
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win.Layer()
}

// one device on one line: a dot for this one, its name, what it runs and when it was last active
func (s *Devices) entry(st *core.State, d api.Device, w, nw, sw int, on bool) string {
	p := ui.P()
	return ui.RowLine(w, on, p.Surface, func(bg color.Color, iw int) string {
		l := ui.L(bg)
		if d.Current {
			l.B(ui.G.Here, p.Accent).Sp(1)
		} else {
			l.Sp(2)
		}
		switch {
		case on && s.rename != nil:
			l.Raw(ui.L(bg).Raw(s.rename.View("rename", nw-2, true, bg).GetContent()).Pad(nw - 2))
		case on:
			l.B(core.Fit(d.Name, nw-2), p.Text)
		default:
			l.T(core.Fit(d.Name, nw-2), p.Text)
		}
		row := ui.L(bg).Raw(l.Pad(nw))
		if sw > 0 {
			row.Sp(2).Raw(ui.L(bg).T(core.Fit(cmp.Or(d.OSName, osName(d.OS)), sw), p.Muted).Pad(sw))
		}
		seen := ui.L(bg).T(core.Ago(d.LastSeenAt, st.Now), p.Faint)
		if d.Current || st.Now.Sub(d.LastSeenAt) < activeFor {
			seen = ui.L(bg).T("Active now", p.Kelp)
		}
		return row.Right(iw, seen)
	})
}
