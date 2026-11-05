package app

import (
	"context"
	"image"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/activity"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/drives"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/gate"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/health"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/overview"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/settings"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var tabNames = []string{"Overview", "Drives", "Activity", "Health", "Settings"}

type checked struct{ checks []core.Check }

type unveil struct{}

const veilFor = 300 * time.Millisecond

type App struct {
	reopen         bool
	fixedOnOpen    bool
	pendingRestart string // an installed dokky this TUI restarts onto once uploads finish
	restartOpen    bool
	everConnected  bool
	autoStarted    bool
	st             *core.State
	deps           core.Deps
	tabs           []core.Screen
	active         int
	gate           core.Screen
	windows        []core.Screen
	toast          *ui.Toast
	pending        []core.Toast
	start          time.Time
	ticking        bool
	hits           ui.Hits
	caret          *image.Point // the typed-in field's caret, for the terminal's bar cursor
	events         <-chan client.Event
	stop           context.CancelFunc

	// blank until the first dial answers or veilFor passes
	veil, failed      bool
	dialing, starting bool
	updateChecked     bool
	backoff           time.Duration
	heard             time.Time // last event from the daemon
	lostAt            time.Time // when a live connection dropped
}

// the TUI ended to open again
func (a *App) Reopening() bool { return a.reopen }

func New(deps core.Deps) *App {
	now := time.Now()
	return &App{
		st:    &core.State{Deps: deps, Now: now},
		deps:  deps,
		tabs:  []core.Screen{overview.New(), drives.New(), activity.New(), health.New(), settings.New()},
		start: now,
		veil:  true,
	}
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, dial(), tea.Tick(veilFor, func(time.Time) tea.Msg { return unveil{} }))
}

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
	case unveil:
		a.veil = false
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
	case latest:
		a.onLatest(m)
	case quiet:
		return a.onQuiet(m)
	case refreshed:
		return a.onRefreshed(m)
	case core.Reply:
		if m.Epoch != a.st.Epoch {
			return nil
		}
		if notRunning(m.Err) {
			return a.onLost(lost{epoch: a.st.Epoch})
		}
		return a.broadcast(m)
	case core.RunChecks:
		return a.runChecks()
	case checked:
		a.st.Checks, a.st.Checked, a.st.Checking = m.checks, a.st.Now, false
		// like dokky fix, every time dokky opens
		if !a.fixedOnOpen {
			a.fixedOnOpen = true
			return a.fixAll(true)
		}
	case found:
		return a.onFound(m)
	case installed:
		return a.onInstalled(m)
	case restartNow:
		return a.onRestartNow()
	case core.FixAll:
		return a.fixAll(m.Auto)
	case core.Recheck:
		a.fixedOnOpen = false
		return a.runChecks()
	case fixed:
		return a.onFixed(m)
	case core.DriveDone:
		a.st.Busy.Done(m.ID)
		if m.Err != nil {
			if notRunning(m.Err) {
				return a.onLost(lost{epoch: a.st.Epoch})
			}
			if core.NoDriver(m.Err) {
				return offerDriver()
			}
			return core.Fail(m.Err)
		}
	case core.Reopen:
		a.reopen = true
		return tea.Quit
	case core.NeedDriver:
		return offerDriver()
	case driverDone:
		return a.onDriverDone(m)
	case core.Toast:
		return a.post(m)
	case toastGone:
		if a.toast != nil && a.toast.At.Equal(m.at) {
			return a.dropToast()
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
		return a.nextToast()
	case core.SwitchTab:
		return a.switchTab(m)
	case menuRun:
		if a.top() == m.owner {
			return a.key(core.Press(m.key))
		}
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
	return tea.Batch(a.nextToast(), a.checkUpdate(), a.restartWhenIdle())
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

func (a *App) setTab(i int) tea.Cmd { return a.switchTab(core.SwitchTab{Tab: i}) }

func (a *App) switchTab(m core.SwitchTab) tea.Cmd {
	if m.Tab < 0 || m.Tab >= len(a.tabs) || len(a.windows) > 0 || a.gate != nil {
		return nil
	}
	a.active = m.Tab
	return a.broadcast(m)
}

func (a *App) capturing() bool {
	c, ok := a.top().(core.Capturer)
	return ok && c.Capturing()
}

func (a *App) key(k tea.KeyPressMsg) tea.Cmd {
	var next tea.Cmd
	if a.toast != nil && a.toast.Bad {
		next = a.dropToast()
	}
	return tea.Batch(next, a.press(k))
}

func (a *App) press(k tea.KeyPressMsg) tea.Cmd {
	s := strings.ToLower(k.String())
	if s == "ctrl+c" {
		return a.quit()
	}
	switch {
	case a.small():
		if s == "q" {
			return a.quit()
		}
		return nil
	case !a.st.Connected && len(a.windows) == 0:
		switch s {
		case "q", "esc":
			return a.quit()
		case "enter":
			if a.failed {
				return a.startDaemon()
			}
		}
		return nil
	case len(a.windows) > 0 || a.gate != nil || a.capturing():
		if s == "q" && len(a.windows) == 0 && !a.capturing() {
			return a.quit()
		}
		if _, open := a.top().(*menu); s == "m" && len(a.windows) > 0 && !open && !a.capturing() {
			return a.openMenu()
		}
		return a.updateTop(k)
	}
	switch s {
	case "q":
		return a.quit()
	case "m":
		return a.openMenu()
	case "u":
		if a.updateReady() {
			a.windows = append(a.windows, &update{btn: 1})
			return nil
		}
	case "1", "2", "3", "4", "5":
		return a.setTab(int(s[0] - '1'))
	case "left":
		return a.setTab(max(0, a.active-1))
	case "right":
		return a.setTab(min(len(a.tabs)-1, a.active+1))
	}
	return a.updateTop(k)
}

func (a *App) runChecks() tea.Cmd {
	if a.st.Checking || a.deps.Doctor == nil {
		return nil
	}
	a.st.Checking = true
	doctor := a.deps.Doctor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return checked{doctor(ctx)}
	}
}

// the header spins while something is in flight
func (a *App) busy() bool {
	st := a.st
	if st.Snap.Account.Pending || st.Checking || st.Fixing || st.Updating != "" && st.Updating != "Update ready" || st.Act.Down > 0 || st.Act.Up > 0 {
		return true
	}
	for _, d := range st.Snap.Drives {
		if d.Uploads > 0 {
			return true
		}
	}
	return false
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
		return !a.veil && (a.starting || !a.failed) && !ui.Reduced()
	}
	if a.busy() && !ui.Reduced() {
		return true
	}
	for _, w := range a.windows {
		if w.Animating(a.st) {
			return true
		}
	}
	return a.top().Animating(a.st)
}
