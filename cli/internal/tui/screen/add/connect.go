package add

import (
	"context"
	"errors"
	"net/url"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type runner struct {
	params   api.NewDrive
	progress api.AddProgress
	req      core.Req
	cancel   context.CancelFunc
	done     *api.Drive
	failed   string
	at       string // the focused button
}

func (r *runner) working() bool { return r.req != 0 }

func (r *runner) checked() int {
	if r.done != nil || r.progress.Done {
		return 3
	}
	return r.progress.Step
}

// closing the call's connection makes the daemon undo what it saved
func (r *runner) stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.req, r.cancel = 0, nil
}

func (r *runner) start(st *core.State, p api.NewDrive) tea.Cmd {
	r.stop()
	ctx, cancel := context.WithCancel(context.Background())
	*r = runner{params: p, req: core.NextReq(), cancel: cancel}
	return core.DoCtx(ctx, st, r.req, func(ctx context.Context, c *client.Client) (any, error) { return c.AddDrive(ctx, p) })
}

func (a *Add) updateConnect(msg tea.Msg, st *core.State) tea.Cmd {
	r := &a.run
	switch m := msg.(type) {
	case core.Reply:
		if m.Req != r.req {
			return nil
		}
		r.req, r.cancel = 0, nil
		if m.Err != nil {
			if !errors.Is(m.Err, context.Canceled) {
				r.failed = core.Report(m.Err)
			}
			if core.NoDriver(m.Err) {
				return core.Send(core.NeedDriver{})
			}
			return nil
		}
		d := m.Val.(api.Drive)
		r.done = &d
		return core.Send(core.Recheck{})
	case tea.KeyPressMsg:
		if r.working() {
			return nil
		}
		r.at = a.focused(st)
		k := m.String()
		if ui.Pressed(k, r.at) {
			return a.press(strings.TrimPrefix(r.at, "btn:"), st)
		}
		ui.Walk(a.stops(st), &r.at, k)
	}
	return nil
}

// Cancel while it mounts; then Try again, or Done with a way to open the drive
func (a *Add) connectBar(st *core.State) []ui.Btn {
	switch r := &a.run; {
	case r.working():
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "add", Label: ui.Spin(st.T) + " Adding", Primary: true}}
	case r.failed != "":
		return []ui.Btn{{ID: "back", Label: "Back"}, {ID: "retry", Label: "Try again", Primary: true}}
	case r.done != nil && r.done.State == api.Connected:
		return []ui.Btn{{ID: "done", Label: "Done"}, {ID: "finder", Label: "Open in " + core.FileManager(), Primary: true}}
	}
	return []ui.Btn{{ID: "done", Label: "Done", Primary: true}}
}

func (a *Add) finish(open bool) tea.Cmd {
	if r := a.run; open && r.done != nil && r.done.State == api.Connected {
		return tea.Batch(core.Close(a), core.OpenFolder(r.done.Folder))
	}
	return core.Close(a)
}

func (a *Add) where() string {
	if a.account != nil {
		return a.account.Name
	}
	o := a.sign.draft
	if h := o["host"]; h != "" {
		return h
	}
	if u, err := url.Parse(o["url"]); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return a.prov.Name
}

func (a *Add) viewConnect(st *core.State, win *ui.Window) int {
	p := ui.P()
	r := &a.run
	cw := win.Inner()
	name := r.params.Name
	win.Title, win.Sub = "Mounting "+name, "This takes a few seconds."
	switch {
	case r.done != nil && r.done.State == api.Connected:
		win.Ok, win.Title, win.Sub = true, name+" is ready", "It is in "+core.FileManager()+" now."
		if runtime.GOOS == "darwin" {
			win.Sub = "It is in Finder now, under Dokky."
		}
	case r.done != nil:
		win.Ok, win.Title, win.Sub = true, name+" is added", "Mount it when you need it."
	case r.failed != "":
		win.Bad, win.Title, win.Sub = true, "Could not add "+name, r.failed
	}
	steps := [3][2]string{{"Checking your sign-in", "Signed in"}, {"Reaching " + a.where(), "Reached " + a.where()}, {"Opening your files", "Files ready"}}
	if !r.params.AtLogin {
		steps[2] = [2]string{"Saving the drive", "Saved"}
	}
	body := ui.NewBox()
	n := r.checked()
	for i, s := range steps {
		l := ui.L(p.Surface)
		switch {
		case i < n:
			l.B(ui.G.Check, p.Kelp).Sp(1).T(s[1], p.Text)
		case i == n && r.failed != "":
			l.B(ui.G.Cross, p.Coral).Sp(1).T(s[0], p.Coral)
		case i == n && r.working():
			l.B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T(s[0], p.Muted)
		default:
			l.Sp(2).T(s[0], p.Faint)
		}
		body.Put(0, i, l.Pad(cw))
	}
	win.Body = body
	return 3
}
