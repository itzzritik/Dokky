package add

import (
	"context"
	"errors"
	"net/url"
	"runtime"

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
	btn      int
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
				r.failed = core.Safe(m.Err.Error())
			}
			return nil
		}
		d := m.Val.(api.Drive)
		r.done = &d
		a.from, a.movedAt = a.progress(st), st.Now
		if d.State != api.Connected {
			r.btn = 1
		}
	case core.Click:
		switch m.ID {
		case "btn:open":
			r.btn = 0
			return a.finish()
		case "btn:done":
			r.btn = 1
			return a.finish()
		}
	case tea.KeyPressMsg:
		switch {
		case r.failed != "" && m.String() == "enter":
			return r.start(st, r.params)
		case r.done == nil:
		case m.String() == "left" || m.String() == "right" || m.String() == "tab":
			if r.done.State == api.Connected {
				r.btn = 1 - r.btn
			}
		case m.String() == "enter":
			return a.finish()
		}
	}
	return nil
}

func (a *Add) finish() tea.Cmd {
	r := &a.run
	if r.btn == 0 && r.done != nil && r.done.State == api.Connected {
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
	win.Title, win.Sub = "Connecting "+name, "This takes a few seconds."
	switch {
	case r.done != nil && r.done.State == api.Connected:
		win.Ok, win.Title, win.Sub = true, name+" is ready", "It is in "+core.FileManager()+" now."
		if runtime.GOOS == "darwin" {
			win.Sub = "It is in Finder now, under Dokky."
		}
	case r.done != nil:
		win.Ok, win.Title, win.Sub = true, name+" is added", "Connect it when you need it."
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
			l.B(ui.Spin(st.T), p.Accent).Sp(1).T(s[0], p.Muted)
		default:
			l.Sp(2).T(s[0], p.Faint)
		}
		body.Put(0, i, l.Pad(cw))
	}
	rows := 3
	if r.done != nil {
		x := 0
		if r.done.State == api.Connected {
			label := "Open in " + core.FileManager()
			body.Add(0, 4, ui.Button("btn:open", label, true, false, r.btn == 0))
			x = ui.ButtonWidth(label) + 2
		}
		body.Add(x, 4, ui.Button("btn:done", "Done", false, false, r.btn == 1))
		rows = 5
	}
	win.Body = body
	return rows
}
