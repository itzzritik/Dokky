package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/daemon"
	"github.com/itzzritik/dokky/cli/internal/doctor"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/rclone/rclone/fs"
)

const (
	callTimeout = 2 * time.Minute
	pollEvery   = 2 * time.Second
)

type Daemon interface {
	Call(ctx context.Context, method string, params, result any) error
	Events(ctx context.Context) (<-chan ipc.Event, error)
}

type tab int

const (
	tabHome tab = iota
	tabServices
	tabTransfers
	tabSettings
	tabDevices
)

var tabNames = []string{"Home", "Services", "Transfers", "Settings", "Devices"}

type serviceItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type (
	statusMsg   daemon.StatusReply
	servicesMsg []serviceItem
	errMsg      struct{ err error }
	tickMsg     struct{}
	eventsMsg   struct{ ch <-chan ipc.Event }
	eventMsg    struct {
		ch <-chan ipc.Event
		ev ipc.Event
		ok bool
	}
	pollMsg struct {
		status    daemon.StatusReply
		transfers []transfer
		err       error
	}
)

type app struct {
	d      Daemon
	ctx    context.Context
	stop   context.CancelFunc
	tab    tab
	width  int
	height int
	bg     *tea.BackgroundColorMsg

	status   daemon.StatusReply
	services []serviceItem
	events   <-chan ipc.Event
	polling  bool
	notice   string
	warning  string
	warned   bool

	checkDriver func() string
	driverFix   string

	home  home
	svc   services
	xfers transfers
	set   settings
	dev   devices
}

var (
	bold   = lipgloss.NewStyle().Bold(true)
	faint  = lipgloss.NewStyle().Faint(true)
	red    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
)

func Run(d Daemon) error {
	a := newApp(d)
	defer a.stop()
	_, err := tea.NewProgram(a).Run()
	return err
}

func newApp(d Daemon) *app {
	ctx, stop := context.WithCancel(context.Background())
	return &app{d: d, ctx: ctx, stop: stop, checkDriver: driverFix, home: home{pending: map[string]string{}}}
}

func (a *app) Init() tea.Cmd {
	return tea.Batch(a.poll(), a.fetchServices(), a.subscribe(), tick(), tea.RequestBackgroundColor, a.do("sync.now", nil, nil))
}

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		a.bg = &msg
	case statusMsg:
		a.setStatus(daemon.StatusReply(msg))
	case pollMsg:
		a.polling = false
		if msg.err != nil {
			a.notice = msg.err.Error()
			return a, nil
		}
		a.setStatus(msg.status)
		if msg.transfers != nil {
			a.xfers.list = msg.transfers
		}
		return a, nil
	case servicesMsg:
		a.services = msg
	case devicesMsg:
		a.dev.list, a.dev.loaded = msg, true
	case errMsg:
		a.notice = msg.err.Error()
	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if !a.polling {
			cmds = append(cmds, a.poll())
		}
		if a.events == nil {
			cmds = append(cmds, a.subscribe())
		}
		if a.svc.mode == svcWorking {
			cmds = append(cmds, a.oauthPoll())
		}
		return a, tea.Batch(cmds...)
	case eventsMsg:
		a.events = msg.ch
		if msg.ch != nil {
			return a, listen(msg.ch)
		}
		return a, nil
	case eventMsg:
		if !msg.ok {
			a.events = nil
			return a, nil
		}
		var st daemon.StatusReply
		if msg.ev.Event == "status" && json.Unmarshal(msg.ev.Data, &st) == nil {
			a.setStatus(st)
		}
		return a, listen(msg.ch)
	case tea.KeyPressMsg:
		return a, a.key(msg)
	}
	return a, tea.Batch(a.homeMsg(msg), a.svcMsg(msg), a.setMsg(msg))
}

func (a *app) key(k tea.KeyPressMsg) tea.Cmd {
	a.notice, a.warning = "", ""
	if k.String() == "ctrl+c" {
		return tea.Quit
	}
	if !a.capturing() {
		switch k.String() {
		case "q":
			return tea.Quit
		case "tab":
			return a.switchTab(1)
		case "shift+tab":
			return a.switchTab(len(tabNames) - 1)
		}
	}
	switch a.tab {
	case tabHome:
		return a.homeKey(k)
	case tabServices:
		return a.svcKey(k)
	case tabSettings:
		return a.setKey(k)
	case tabDevices:
		return a.devKey(k)
	}
	return nil
}

func (a *app) capturing() bool {
	return a.home.add != nil || a.svc.mode != svcList || a.set.edit != nil || a.dev.revoking != nil
}

func (a *app) switchTab(step int) tea.Cmd {
	a.tab = tab((int(a.tab) + step) % len(tabNames))
	switch a.tab {
	case tabTransfers:
		return a.poll()
	case tabSettings:
		return a.fetchSettings()
	case tabServices:
		return a.fetchServices()
	case tabDevices:
		return a.fetchDevices()
	}
	return nil
}

func (a *app) setStatus(st daemon.StatusReply) {
	a.status = st
	if !a.warned && st.MinFreeBytes > 0 && st.FreeBytes < st.MinFreeBytes {
		a.warned = true
		a.warning = fmt.Sprintf("Low disk space: %s free, below the %s dokky keeps free. Large copies may fail with \"disk full\".",
			size(int64(st.FreeBytes)), size(int64(st.MinFreeBytes)))
	}
	a.driverFix = ""
	for _, m := range st.Mounts {
		if m.State == "error" {
			a.driverFix = a.checkDriver()
			break
		}
	}
}

func (a *app) View() tea.View {
	var b strings.Builder
	b.WriteString(bold.Render("dokky") + "  ")
	for i, name := range tabNames {
		if tab(i) == a.tab {
			b.WriteString(bold.Underline(true).Render("["+name+"]") + " ")
		} else {
			b.WriteString(faint.Render(" "+name+" ") + " ")
		}
	}
	b.WriteString("\n\n")
	var body, help string
	switch a.tab {
	case tabHome:
		body, help = a.homeView()
	case tabServices:
		body, help = a.svcView()
	case tabTransfers:
		body, help = a.xfersView(), ""
	case tabSettings:
		body, help = a.setView()
	case tabDevices:
		body, help = a.devView()
	}
	b.WriteString(body + "\n")
	if a.warning != "" {
		b.WriteString("\n" + yellow.Render(a.warning) + "\n")
	}
	if a.notice != "" {
		b.WriteString("\n" + red.Render(a.notice) + "\n")
	}
	if !a.capturing() {
		help = strings.TrimPrefix(help+" · tab switch · q quit", " · ")
	}
	b.WriteString("\n" + faint.Render(help))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (a *app) callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, callTimeout)
}

func fetch[T any](a *app, method string, params any, wrap func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		var out T
		if err := a.d.Call(ctx, method, params, &out); err != nil {
			return errMsg{err}
		}
		return wrap(out)
	}
}

func (a *app) do(method string, params any, then tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		if err := a.d.Call(ctx, method, params, nil); err != nil {
			return errMsg{err}
		}
		if then == nil {
			return nil
		}
		return then()
	}
}

func (a *app) poll() tea.Cmd {
	a.polling = true
	withTransfers := a.tab == tabTransfers
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		var m pollMsg
		if m.err = a.d.Call(ctx, "status", nil, &m.status); m.err == nil && withTransfers {
			m.transfers = []transfer{}
			m.err = a.d.Call(ctx, "transfers", nil, &m.transfers)
		}
		return m
	}
}

func (a *app) refreshStatus() tea.Cmd {
	return fetch(a, "status", nil, func(st daemon.StatusReply) tea.Msg { return statusMsg(st) })
}

func (a *app) fetchServices() tea.Cmd {
	return fetch(a, "service.list", nil, func(s []serviceItem) tea.Msg { return servicesMsg(s) })
}

func (a *app) subscribe() tea.Cmd {
	return func() tea.Msg {
		ch, err := a.d.Events(a.ctx)
		if err != nil {
			return eventsMsg{}
		}
		return eventsMsg{ch}
	}
}

func listen(ch <-chan ipc.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return eventMsg{ch: ch, ev: ev, ok: ok}
	}
}

func (a *app) newForm(groups ...*huh.Group) *huh.Form {
	f := huh.NewForm(groups...)
	if a.width > 0 {
		f = f.WithWidth(min(a.width, 100)).WithHeight(max(a.height-8, 6))
	}
	if a.bg != nil {
		f.Update(*a.bg)
	}
	return f
}

func driverFix() string {
	c := doctor.Driver(doctor.Env{GOOS: runtime.GOOS, LookPath: exec.LookPath, Stat: os.Stat, ReadFile: os.ReadFile, Access: doctor.OpenReadWrite})
	if c.OK {
		return ""
	}
	return c.Fix
}

func size(n int64) string { return fs.SizeSuffix(n).ByteUnit() }

func stateWord(state string) string {
	switch state {
	case "mounted":
		return green.Render(state)
	case "mounting", "unmounting":
		return yellow.Render(state)
	case "error":
		return red.Render(state)
	case "removed":
		return yellow.Render("removed on another device")
	}
	return faint.Render(state)
}

func clamp(i, n int) int { return max(0, min(i, n-1)) }

func cursor(on bool) string {
	if on {
		return "> "
	}
	return "  "
}
