package app

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/activity"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/devices"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/drives"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/overview"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/settings"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var tabNames = []string{"Overview", "Drives", "Activity", "Devices", "Settings"}

type toastGone struct{ at time.Time }

type App struct {
	st      *core.State
	deps    core.Deps
	tabs    []core.Screen
	active  int
	gate    core.Screen
	windows []core.Screen
	toast   *ui.Toast
	start   time.Time
	ticking bool
	hits    ui.Hits
	events  <-chan client.Event
	stop    context.CancelFunc

	dialing, starting bool
}

func New(deps core.Deps) *App {
	now := time.Now()
	return &App{
		st:    &core.State{Deps: deps, Now: now},
		deps:  deps,
		tabs:  []core.Screen{overview.New(now), drives.New(), activity.New(), devices.New(), settings.New()},
		start: now,
	}
}

func (a *App) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, dial()) }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	a.st.Now = time.Now()
	a.st.T = a.st.Now.Sub(a.start).Seconds()
	cmd := a.update(msg)
	if !a.ticking && a.animating() {
		a.ticking = true
		cmd = tea.Batch(cmd, ui.Tick())
	}
	return a, cmd
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.BackgroundColorMsg:
		ui.SetTerminal(m.Color)
	case tea.WindowSizeMsg:
		a.st.W, a.st.H, a.st.Wide = m.Width, m.Height, m.Width >= 100
	case ui.TickMsg:
		a.ticking = false
		if a.animating() {
			a.ticking = true
			return ui.Tick()
		}
	case connected:
		return a.onConnected(m)
	case lost:
		return a.onLost(m)
	case redial:
		a.dialing = false
		return dial()
	case started:
		a.starting = false
		if m.err != nil {
			return core.Fail(m.err)
		}
		return dial()
	case event:
		return a.onEvent(m)
	case core.Reply:
		if m.Epoch != a.st.Epoch {
			return nil
		}
		if notRunning(m.Err) {
			return a.onLost(lost{epoch: a.st.Epoch})
		}
		return a.broadcast(m)
	case core.DriveDone:
		a.st.Busy.Done(m.ID)
		if m.Err != nil {
			if notRunning(m.Err) {
				return a.onLost(lost{epoch: a.st.Epoch})
			}
			return core.Fail(m.Err)
		}
	case core.Toast:
		a.toast = &ui.Toast{Text: m.Text, Bad: m.Bad, At: a.st.Now}
		if !m.Bad {
			at := a.st.Now
			return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return toastGone{at} })
		}
	case toastGone:
		if a.toast != nil && a.toast.At.Equal(m.at) {
			a.toast = nil
		}
	case core.OpenWindow:
		if _, ok := m.Screen.(*gate.Clashes); ok && a.hasClashWindow() {
			return nil
		}
		a.windows = append(a.windows, m.Screen)
	case core.CloseWindow:
		if i := slices.Index(a.windows, m.Screen); i >= 0 {
			a.windows = slices.Delete(a.windows, i, i+1)
		}
	case core.SwitchTab:
		return a.setTab(m.Tab)
	case tea.KeyPressMsg:
		return a.key(m)
	case tea.PasteMsg:
		return a.updateTop(m)
	case tea.MouseClickMsg:
		return a.click(m.Mouse())
	case tea.MouseWheelMsg:
		k := "down"
		if m.Mouse().Button == tea.MouseWheelUp {
			k = "up"
		}
		return a.updateTop(core.Press(k))
	default:
		return a.broadcast(msg)
	}
	return nil
}

func (a *App) setSnap(s api.State) tea.Cmd {
	a.st.Snap = s
	ui.SetTheme(s.Settings.Theme)
	ui.SetMotion(s.Settings.Motion)
	if wantGate(a.st) {
		if a.gate == nil {
			a.gate = gate.New(a.st.Now)
		}
	} else {
		a.gate = nil
	}
	if len(s.Clashes) > 0 && s.Account.Pending && !a.hasClashWindow() {
		a.windows = append(a.windows, gate.NewClashes(s.Clashes))
	}
	return nil
}

func wantGate(st *core.State) bool {
	return st.Connected && !st.Snap.Settings.Chosen && !st.Snap.Account.LoggedIn && len(st.Snap.Drives) == 0
}

func (a *App) hasClashWindow() bool {
	for _, w := range a.windows {
		if _, ok := w.(*gate.Clashes); ok {
			return true
		}
	}
	return false
}

// the window on top, else the gate, else the tab
func (a *App) top() core.Screen {
	if n := len(a.windows); n > 0 {
		return a.windows[n-1]
	}
	if a.gate != nil {
		return a.gate
	}
	return a.tabs[a.active]
}

func (a *App) updateTop(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch n := len(a.windows); {
	case n > 0:
		a.windows[n-1], cmd = a.windows[n-1].Update(msg, a.st)
	case a.gate != nil:
		a.gate, cmd = a.gate.Update(msg, a.st)
	default:
		a.tabs[a.active], cmd = a.tabs[a.active].Update(msg, a.st)
	}
	return cmd
}

func (a *App) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	for i := range a.windows {
		a.windows[i], cmd = a.windows[i].Update(msg, a.st)
		cmds = append(cmds, cmd)
	}
	if a.gate != nil {
		a.gate, cmd = a.gate.Update(msg, a.st)
		cmds = append(cmds, cmd)
	}
	for i := range a.tabs {
		a.tabs[i], cmd = a.tabs[i].Update(msg, a.st)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (a *App) setTab(i int) tea.Cmd {
	if i < 0 || i >= len(a.tabs) || len(a.windows) > 0 || a.gate != nil {
		return nil
	}
	a.active = i
	return a.broadcast(core.SwitchTab{Tab: i})
}

func (a *App) capturing() bool {
	c, ok := a.top().(core.Capturer)
	return ok && c.Capturing()
}

func (a *App) key(k tea.KeyPressMsg) tea.Cmd {
	s := strings.ToLower(k.String())
	if s == "ctrl+c" {
		return a.quit()
	}
	if a.toast != nil && a.toast.Bad {
		a.toast = nil
	}
	switch {
	case a.small():
		if s == "q" {
			return a.quit()
		}
		return nil
	case !a.st.Connected:
		switch s {
		case "q", "esc":
			return a.quit()
		case "enter":
			return a.startDaemon()
		}
		return nil
	case len(a.windows) > 0 || a.gate != nil || a.capturing():
		if s == "q" && len(a.windows) == 0 && !a.capturing() {
			return a.quit()
		}
		return a.updateTop(k)
	}
	switch s {
	case "q":
		return a.quit()
	case "1", "2", "3", "4", "5":
		return a.setTab(int(s[0] - '1'))
	case "tab":
		return a.setTab((a.active + 1) % len(a.tabs))
	case "shift+tab":
		return a.setTab((a.active + len(a.tabs) - 1) % len(a.tabs))
	case "left", "right":
		if h, ok := a.tabs[a.active].(core.Horizontal); !ok || !h.Horizontal(a.st) {
			d := 1
			if s == "left" {
				d = -1
			}
			return a.setTab(max(0, min(len(a.tabs)-1, a.active+d)))
		}
	}
	return a.updateTop(k)
}

func (a *App) quit() tea.Cmd {
	if a.stop != nil {
		a.stop()
	}
	return tea.Quit
}

func (a *App) small() bool { return a.st.W < 80 || a.st.H < 24 }

func (a *App) animating() bool {
	if a.small() {
		return false
	}
	if a.toast != nil && a.toast.Moving(a.st.Now) {
		return true
	}
	if !a.st.Connected {
		return a.starting && !ui.Reduced()
	}
	if a.st.Snap.Account.Pending && !ui.Reduced() {
		return true
	}
	for _, w := range a.windows {
		if w.Animating(a.st) {
			return true
		}
	}
	return a.top().Animating(a.st)
}
