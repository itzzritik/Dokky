package devices

import (
	"cmp"
	"context"
	"image/color"
	"slices"
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

// one background sync is 15 minutes, so a device seen in the last 16 is still on
const (
	activeFor = 16 * time.Minute
	idleFor   = 90 * 24 * time.Hour
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
			// this device first, the rest by when they were last active
			slices.SortStableFunc(list, func(x, y api.Device) int {
				if x.Current != y.Current {
					if x.Current {
						return -1
					}
					return 1
				}
				return y.LastSeenAt.Compare(x.LastSeenAt)
			})
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
		s.rename = ui.NewField("", shown(d.Name), "Device name", false)
		s.rename.Bare = true
	case "d":
		if d.Current {
			return tea.Batch(core.Close(s), RemoveSelf())
		}
		return s.remove(st, d)
	}
	return nil
}

func (s *Devices) Capturing() bool { return s.rename != nil }

func (s *Devices) save(st *core.State) tea.Cmd {
	name := strings.TrimSpace(s.rename.Value())
	s.rename = nil
	if s.sel >= len(s.list) || name == "" || name == shown(s.list[s.sel].Name) {
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

const removeLine = "It signs out and erases its dokky data once its transfers finish."

// removing this device, from here or from Settings
func RemoveSelf() tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Remove this device?",
		Line:     "Your files stay in your storage.",
		Lines:    []string{removeLine},
		Do:       "Remove",
		Run: func(st *core.State) tea.Cmd {
			c := st.C
			return func() tea.Msg {
				if err := c.RevokeDevice(context.Background(), "self"); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return nil
			}
		},
	})
}

// an offline device keeps its sign-ins until it comes back, so the accounts it holds are named
func (s *Devices) remove(st *core.State, d api.Device) tea.Cmd {
	c, name := st.C, core.Safe(shown(d.Name))
	offline := st.Now.Sub(d.LastSeenAt) >= activeFor
	ask := func(accounts []string) tea.Msg {
		lines := []string{removeLine}
		if offline {
			l := "It's offline, so it erases itself when it next comes online."
			if len(accounts) > 0 {
				l += " Until then it keeps its sign-ins for: " + strings.Join(accounts, ", ") + "."
			}
			lines = append(lines, l)
		}
		lines = append(lines, "Lost or stolen? Remove it from the dashboard.")
		return confirm.Open(&confirm.Confirm{
			Question: "Remove " + name + "?",
			Line:     "Your files stay in your storage.",
			Lines:    lines,
			Do:       "Remove",
			Run: func(st *core.State) tea.Cmd {
				return tea.Sequence(func() tea.Msg {
					if err := c.RevokeDevice(context.Background(), d.ID); err != nil {
						return core.Toast{Text: core.Report(err), Bad: true}
					}
					return core.Toast{Text: "Removed " + name}
				}, s.load(st))
			},
		})()
	}
	return func() tea.Msg {
		if !offline {
			return ask(nil)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		refs, _ := c.Accounts(ctx)
		names := make([]string, 0, len(refs))
		for _, r := range refs {
			names = append(names, core.Safe(r.Name))
		}
		slices.Sort(names)
		return ask(names)
	}
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
	return append(acts, core.Action{Key: "r", Label: "rename"}, core.Action{Key: "d", Label: "remove", Danger: true}, core.Action{Key: "esc", Label: "close"})
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

const cardH = 5

// hostnames end in .local on macOS; the name people know doesn't
func shown(name string) string { return strings.TrimSuffix(name, ".local") }

func (s *Devices) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	win := &ui.Window{W: min(76, maxW), Title: "Your devices"}
	if e := st.Snap.Account.Email; e != "" {
		win.Sub = "Signed in as " + e
	}
	cw := win.Inner()
	body := ui.NewBox()
	rows := 1
	switch {
	case !st.Snap.Account.LoggedIn:
		body.Put(0, 0, win.Text("Sign in to see your devices.", p.Muted))
	case s.list == nil:
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Loading", p.Muted).Pad(cw))
	default:
		s.sel = max(0, min(s.sel, len(s.list)-1))
		fit := max(1, (win.Room(maxH)+1)/cardH)
		top := max(0, s.sel-fit+1)
		for i := top; i < len(s.list) && i < top+fit; i++ {
			y := (i - top) * cardH
			body.Add(0, y, s.card(st, s.list[i], cw, i == s.sel))
			rows = y + cardH
		}
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win.Layer()
}

// "Apple Silicon" or "Intel" on a Mac, the chip's usual name elsewhere
func chip(d api.Device) string {
	switch {
	case d.OS == "darwin" && d.Arch == "arm64":
		return "Apple Silicon"
	case d.OS == "darwin":
		return "Intel"
	case d.Arch == "amd64":
		return "x64"
	case d.Arch == "arm64":
		return "ARM64"
	}
	return d.Arch
}

// a device as a soft card: its name and where it stands, then what it runs and its dokky version
func (s *Devices) card(st *core.State, d api.Device, w int, on bool) *lipgloss.Layer {
	p := ui.P()
	return ui.Card("row:"+d.ID, w, on, ui.Mix(p.Surface, p.Text, .045), p.Surface, func(bg color.Color, iw int) []string {
		name := ui.L(bg)
		if on && s.rename != nil {
			name.Raw(ui.L(bg).Raw(s.rename.View("rename", iw-18, true, bg).GetContent()).Pad(iw - 18))
		} else {
			name.B(core.Fit(shown(d.Name), iw-18), p.Text)
		}
		status := ui.L(bg)
		switch {
		case d.Current:
			status.B(ui.G.Here, p.Accent).Sp(1).T("This device", p.Accent)
		case st.Now.Sub(d.LastSeenAt) < activeFor:
			status.B(ui.G.Here, p.Kelp).Sp(1).T("Active now", p.Kelp)
		case st.Now.Sub(d.LastSeenAt) >= idleFor:
			status.T("Not seen in 3 months", p.Buoy)
		default:
			status.T("Seen "+core.Ago(d.LastSeenAt, st.Now), p.Faint)
		}
		system := ui.L(bg).T(cmp.Or(d.OSName, osName(d.OS)), p.Muted)
		if c := chip(d); c != "" {
			system.Sp(2).T(c, p.Faint)
		}
		meta := ui.L(bg)
		if d.Version != "" {
			meta.T("v"+d.Version, p.Faint)
		}
		return []string{name.Right(iw, status), system.Right(iw, meta), mountedLine(st, d, iw, bg)}
	})
}

// the drives this device has mounted, by name
func mountedLine(st *core.State, d api.Device, w int, bg color.Color) string {
	p := ui.P()
	var names []string
	for _, id := range d.Mounted {
		if x, ok := st.Drive(id); ok {
			names = append(names, x.Name)
		}
	}
	if len(names) == 0 {
		return ui.L(bg).T("No drives mounted", p.Faint).String()
	}
	return ui.L(bg).T("Mounted ", p.Faint).T(core.Fit(strings.Join(names, ", "), w-8), p.Muted).String()
}
