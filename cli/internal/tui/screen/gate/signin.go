package gate

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type pollTick struct{ req core.Req }

type SignIn struct {
	btn    int
	req    core.Req
	start  client.LoginStart
	status string // waiting, denied, expired, failed
	err    string
}

// opens the sign-in window and starts the device code flow
func Open(st *core.State) tea.Cmd {
	s := &SignIn{btn: 2}
	return tea.Sequence(core.Open(s), s.begin(st))
}

func (s *SignIn) begin(st *core.State) tea.Cmd {
	s.req, s.status, s.err, s.start = core.NextReq(), "waiting", "", client.LoginStart{}
	return core.Do(st, s.req, func(ctx context.Context, c *client.Client) (any, error) { return c.LoginStart(ctx) })
}

func (s *SignIn) poll(st *core.State) tea.Cmd {
	return core.Do(st, s.req, func(ctx context.Context, c *client.Client) (any, error) { return c.LoginPoll(ctx) })
}

func (s *SignIn) wait() tea.Cmd {
	req := s.req
	return tea.Tick(time.Duration(max(s.start.Interval, 1))*time.Second, func(time.Time) tea.Msg { return pollTick{req} })
}

func (s *SignIn) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	switch m := msg.(type) {
	case pollTick:
		if m.req == s.req && s.status == "waiting" {
			return s, s.poll(st)
		}
	case core.Reply:
		if m.Req != s.req {
			return s, nil
		}
		if m.Err != nil {
			s.status, s.err = "failed", core.Safe(m.Err.Error())
			return s, nil
		}
		switch v := m.Val.(type) {
		case client.LoginStart:
			s.start = v
			if v.Resume {
				return s, s.poll(st)
			}
			return s, s.wait()
		case client.LoginPoll:
			switch v.Status {
			case "pending":
				return s, s.wait()
			case "denied", "expired":
				s.status = v.Status
			case "approved":
				if len(v.Clashes) > 0 {
					return s, tea.Sequence(core.Close(s), core.Open(NewClashes(v.Clashes)))
				}
				return s, tea.Batch(core.Close(s), Choose(st, core.NextReq()), core.Say("Signed in"))
			}
		}
	case core.Click:
		if id, ok := strings.CutPrefix(m.ID, "btn:"); ok {
			return s, s.press(id, st)
		}
	case tea.KeyPressMsg:
		btns := s.buttons()
		s.btn = max(0, min(s.btn, len(btns)-1))
		switch m.String() {
		case "esc":
			return s, s.press("cancel", st)
		case "c":
			return s, s.press("copy", st)
		case "o":
			return s, s.press("open", st)
		case "left":
			s.btn = max(0, s.btn-1)
		case "right":
			s.btn = min(len(btns)-1, s.btn+1)
		case "enter":
			return s, s.press(btns[s.btn].ID, st)
		}
	}
	return s, nil
}

func (s *SignIn) buttons() []ui.Btn {
	switch {
	case s.status != "waiting":
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "retry", Label: "Try again", Primary: true}}
	case s.start.VerifyURL == "":
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}}
	}
	return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "copy", Label: "Copy link"}, {ID: "open", Label: "Open browser", Primary: true}}
}

func (s *SignIn) press(id string, st *core.State) tea.Cmd {
	u := s.start.VerifyURL
	switch {
	case id == "cancel":
		s.req = 0
		return core.Close(s)
	case id == "retry":
		return s.begin(st)
	case id == "copy" && u != "":
		return tea.Batch(tea.SetClipboard(u), core.Say("Link copied"))
	case id == "open" && u != "":
		return func() tea.Msg {
			if err := platform.OpenURL(u); err != nil {
				return core.Toast{Text: "Could not open the browser. Copy the link instead.", Bad: true}
			}
			return nil
		}
	}
	return nil
}

func (s *SignIn) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	p := ui.P()
	btns := s.buttons()
	s.btn = max(0, min(s.btn, len(btns)-1))
	win := &ui.Window{W: min(68, maxW), Title: "Finish in your browser", Sub: "Check that it shows this code.", Shimmer: st.T, Buttons: btns, Active: s.btn}
	body := ui.NewBox()
	rows := 0
	switch {
	case s.status == "denied":
		win.Title, win.Sub, win.Bad = "The sign-in was denied", "Press enter to try again.", true
	case s.status == "expired":
		win.Title, win.Sub, win.Bad = "The code expired", "Press enter to get a new one.", true
	case s.status == "failed":
		win.Title, win.Sub, win.Bad = "Could not sign in", "Press enter to try again.", true
		body.Put(0, 0, win.Text(s.err, p.Coral))
		rows = 1
	case s.start.UserCode == "":
		win.Title, win.Sub = "Signing in", ""
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), p.Accent).Sp(1).T("Waiting for dokky.ritik.me", p.Muted).Pad(win.Inner()))
		rows = 1
	default:
		code := ui.L(p.Surface)
		for _, c := range s.start.UserCode {
			if c == '-' {
				code.T(" - ", p.Faint)
				continue
			}
			code.Raw(ui.Paint(" "+string(c)+" ", p.Ramp[5], p.Cap, true)).Sp(1)
		}
		body.Put(0, 0, code.Pad(win.Inner()))
		body.Put(0, 3, ui.L(p.Surface).B(ui.Spin(st.T), p.Accent).Sp(1).T("Waiting for you to approve", p.Muted).Pad(win.Inner()))
		rows = 4
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win.Layer()
}

func (s *SignIn) Actions(*core.State) []core.Action {
	return []core.Action{{Key: "← →", Label: "choose"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "cancel"}}
}

func (s *SignIn) Animating(*core.State) bool { return s.status == "waiting" && !ui.Reduced() }
