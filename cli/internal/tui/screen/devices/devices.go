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
	sel    int    // the device the buttons act on
	at     string // the focused stop: a device's card, then the buttons
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

func rowID(d api.Device) string { return "row:" + d.ID }

// every device's card, then the buttons
func (s *Devices) stops() []string {
	stops := make([]string, 0, len(s.list)+2)
	for _, d := range s.list {
		stops = append(stops, rowID(d))
	}
	if len(s.list) > 0 {
		stops = append(stops, "btn:rename", "btn:remove")
	}
	return stops
}

// focus on a card selects that device; on a button the last one stays selected
func (s *Devices) focus(id string) {
	s.at = id
	for i, d := range s.list {
		if rowID(d) == id {
			s.sel = i
		}
	}
}

// a fresh list keeps focus on the same device where it can, else on the selected card
func (s *Devices) settle() {
	if len(s.list) == 0 {
		return
	}
	s.sel = max(0, min(s.sel, len(s.list)-1))
	if !slices.Contains(s.stops(), s.at) {
		s.at = rowID(s.list[s.sel])
	}
	s.focus(s.at)
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
			s.settle()
		}
	case core.Click:
		return s, s.click(m.ID, st)
	case tea.KeyPressMsg:
		return s, s.key(m, st)
	}
	return s, nil
}

func (s *Devices) click(id string, st *core.State) tea.Cmd {
	if s.rename != nil {
		switch id {
		case "btn:cancel":
			s.rename = nil
		case "btn:save":
			return s.save(st)
		}
		return nil
	}
	if !slices.Contains(s.stops(), id) {
		return nil
	}
	s.focus(id)
	return s.press(id, st)
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
	s.settle()
	switch {
	case k == "r" || k == "d":
		// the letters press the buttons, focus and all
		id := map[string]string{"r": "btn:rename", "d": "btn:remove"}[k]
		s.focus(id)
		return s.press(id, st)
	case k == "enter" && !ui.IsBtn(s.at):
		// enter on a card renames it, the first of its actions
		return s.press("btn:rename", st)
	case ui.Pressed(k, s.at):
		return s.press(s.at, st)
	case ui.Walk(s.stops(), &s.at, k):
		s.focus(s.at)
	}
	return nil
}

// Rename and Remove act on the selected device
func (s *Devices) press(id string, st *core.State) tea.Cmd {
	if s.sel >= len(s.list) {
		return nil
	}
	d := s.list[s.sel]
	switch id {
	case "btn:rename":
		s.rename = ui.NewField("", shown(d.Name), "Device name", false)
		s.rename.Bare = true
	case "btn:remove":
		if d.Current {
			return removeSelf(s)
		}
		return s.remove(st, d)
	}
	return nil
}

// the bar, also listed by the actions menu: Cancel and Save while renaming
func (s *Devices) Buttons(*core.State) []ui.Btn {
	switch {
	case s.rename != nil:
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "save", Label: "Save", Primary: true}}
	case len(s.list) > 0:
		return []ui.Btn{{ID: "rename", Label: "Rename"}, {ID: "remove", Label: "Remove", Danger: true}}
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

// removing this device, from Settings
func RemoveSelf() tea.Cmd { return removeSelf(nil) }

// from, when set, is the Devices window: it stays open under the question and closes only once Remove is pressed
func removeSelf(from core.Screen) tea.Cmd {
	return confirm.Open(&confirm.Confirm{
		Question: "Remove this device?",
		Line:     "Your files stay in your storage.",
		Lines:    []string{removeLine},
		Do:       "Remove",
		Run: func(st *core.State) tea.Cmd {
			c := st.C
			revoke := func() tea.Msg {
				if err := c.RevokeDevice(context.Background(), "self"); err != nil {
					return core.Toast{Text: core.Report(err), Bad: true}
				}
				return nil
			}
			if from == nil {
				return revoke
			}
			return tea.Batch(core.Close(from), revoke)
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

// enter names what it does on the focused stop; r and d stay as quiet shortcuts
func (s *Devices) Actions(*core.State) []core.Action {
	switch {
	case s.rename != nil:
		return []core.Action{{Key: "enter", Label: "save"}, {Key: "esc", Label: "cancel"}}
	case len(s.list) == 0:
		return []core.Action{{Key: "esc", Label: "close"}}
	}
	enter := core.Action{Key: "enter", Label: "rename"}
	if s.at == "btn:remove" {
		enter = core.Action{Key: "enter", Label: "remove", Danger: true}
	}
	move := core.Action{Key: "tab", Label: "move"}
	if len(s.list) > 1 {
		move.Key = "↑↓"
	}
	acts := []core.Action{enter, move}
	if ui.IsBtn(s.at) {
		acts = append(acts, core.Action{Key: "← →", Label: "choose"})
	}
	return append(acts, core.Action{Key: "esc", Label: "close"})
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

func (s *Devices) window(st *core.State, maxW, maxH int) *ui.Window {
	p := ui.P()
	win := &ui.Window{W: min(76, maxW), Title: "Your devices", Buttons: s.Buttons(st), Active: -1}
	switch {
	case s.rename != nil:
		// the name field has focus, so the button enter reaches is lit
		win.Active = win.Index("btn:save")
	case ui.IsBtn(s.at):
		win.Active = win.Index(s.at)
	}
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
		body.Put(0, 0, ui.L(p.Window).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Loading", p.Muted).Pad(cw))
	default:
		sel := max(0, min(s.sel, len(s.list)-1))
		fit := max(1, (win.Room(maxH)+1)/cardH)
		top := max(0, sel-fit+1)
		// the selected card keeps a soft fill while a button has focus, so it's clear what the buttons act on
		focused := s.rename != nil || !ui.IsBtn(s.at)
		for i := top; i < len(s.list) && i < top+fit; i++ {
			y := (i - top) * cardH
			body.Add(0, y, s.card(st, s.list[i], cw, i == sel, i == sel && focused))
			rows = y + cardH
		}
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win
}

func (s *Devices) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return s.window(st, maxW, maxH).Layer()
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
func (s *Devices) card(st *core.State, d api.Device, w int, chosen, focused bool) *lipgloss.Layer {
	p := ui.P()
	fill := ui.Mix(p.Window, p.Text, .045)
	if chosen && !focused {
		fill = ui.Mix(fill, p.Sel, .5)
	}
	return ui.Card(rowID(d), w, focused, fill, p.Window, func(bg color.Color, iw int) []string {
		name := ui.L(bg)
		if chosen && s.rename != nil {
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
