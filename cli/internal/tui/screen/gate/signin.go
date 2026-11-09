package gate

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/screen/vault"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

type pollTick struct{ req core.Req }

// a poll's answer, and the master password step the daemon waits on before it finishes signing in
type polled struct {
	client.LoginPoll
	need string
}

type SignIn struct {
	at     string // the focused button
	req    core.Req
	start  client.LoginStart
	status string // waiting, denied, expired, failed
	err    string
}

// opens the browser; tests swap it so none opens
var openURL = platform.OpenURL

// opens the sign-in window and starts the device code flow
func Open(st *core.State) tea.Cmd {
	s := &SignIn{}
	cmd := s.begin(st)
	s.at = s.primary()
	return tea.Sequence(core.Open(s), cmd)
}

func (s *SignIn) begin(st *core.State) tea.Cmd {
	s.req, s.status, s.err, s.start = core.NextReq(), "waiting", "", client.LoginStart{}
	return core.Do(st, s.req, func(ctx context.Context, c *client.Client) (any, error) { return c.LoginStart(ctx) })
}

// once approved, the state is asked too: the event stream can trail this reply
func (s *SignIn) poll(st *core.State) tea.Cmd {
	return core.Do(st, s.req, func(ctx context.Context, c *client.Client) (any, error) {
		p, err := c.LoginPoll(ctx)
		if err == nil && p.Status == "pending" {
			return polled{LoginPoll: p}, nil
		}
		if snap, serr := c.State(ctx); serr == nil && snap.Account.Pending && snap.Vault.Need != "" {
			return polled{LoginPoll: p, need: snap.Vault.Need}, nil
		}
		return polled{LoginPoll: p}, err
	})
}

// after the master password step: the sign in that waited for it uploads, then settles clashes as usual
func Resume(st *core.State) tea.Cmd {
	s := &SignIn{req: core.NextReq(), status: "waiting"}
	s.at = s.primary()
	return tea.Sequence(core.Open(s), s.poll(st))
}

func (s *SignIn) wait() tea.Cmd {
	req := s.req
	return tea.Tick(time.Duration(max(s.start.Interval, 1))*time.Second, func(time.Time) tea.Msg { return pollTick{req} })
}

func (s *SignIn) Update(msg tea.Msg, st *core.State) (core.Screen, tea.Cmd) {
	was := s.set()
	cmd := s.update(msg, st)
	// a new set of buttons focuses its primary, so enter does what the window now asks
	if s.set() != was {
		s.at = s.primary()
	}
	return s, cmd
}

func (s *SignIn) update(msg tea.Msg, st *core.State) tea.Cmd {
	switch m := msg.(type) {
	case pollTick:
		if m.req == s.req && s.status == "waiting" {
			return s.poll(st)
		}
	case core.Reply:
		if m.Req != s.req {
			return nil
		}
		if m.Err != nil {
			s.status, s.err = "failed", core.Report(m.Err)
			return nil
		}
		switch v := m.Val.(type) {
		case client.LoginStart:
			s.start = v
			if v.Resume {
				return s.poll(st)
			}
			return s.wait()
		case polled:
			if v.need != "" {
				s.req = 0
				return tea.Sequence(core.Close(s), core.Open(vault.SignIn(st, v.need, Resume)))
			}
			switch v.Status {
			case "pending":
				return s.wait()
			case "denied", "expired":
				s.status = v.Status
			case "approved":
				if len(v.Clashes) > 0 {
					return tea.Sequence(core.Close(s), core.Open(NewClashes(v.Clashes)))
				}
				return tea.Batch(core.Close(s), Choose(st, core.NextReq()), core.Say("Signed in"))
			}
		}
	case core.Click:
		if slices.Contains(s.stops(), m.ID) {
			s.at = m.ID
			return s.press(m.ID, st)
		}
	case tea.KeyPressMsg:
		switch k := m.String(); {
		case k == "esc":
			return s.press("btn:cancel", st)
		case k == "c" || k == "o":
			return s.letter(k, st)
		case ui.Pressed(k, s.at):
			return s.press(s.at, st)
		case ui.Walk(s.stops(), &s.at, k):
		}
	}
	return nil
}

// c and o press the link buttons while they show
func (s *SignIn) letter(k string, st *core.State) tea.Cmd {
	id := "btn:copy"
	if k == "o" {
		id = "btn:open"
	}
	if !slices.Contains(s.stops(), id) {
		return nil
	}
	s.at = id
	return s.press(id, st)
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

// the bar, also listed by the actions menu
func (s *SignIn) Buttons(*core.State) []ui.Btn { return s.buttons() }

func (s *SignIn) stops() []string {
	var ids []string
	for _, b := range s.buttons() {
		ids = append(ids, "btn:"+b.ID)
	}
	return ids
}

// which buttons show, to see when they change
func (s *SignIn) set() string { return strings.Join(s.stops(), " ") }

// the primary button, else the only one
func (s *SignIn) primary() string {
	btns := s.buttons()
	for _, b := range btns {
		if b.Primary {
			return "btn:" + b.ID
		}
	}
	return "btn:" + btns[len(btns)-1].ID
}

func (s *SignIn) press(id string, st *core.State) tea.Cmd {
	u := s.start.VerifyURL
	switch {
	case id == "btn:cancel":
		s.req = 0
		return core.Close(s)
	case id == "btn:retry":
		return s.begin(st)
	case id == "btn:copy" && u != "":
		return tea.Batch(tea.SetClipboard(u), core.Say("Link copied"))
	case id == "btn:open" && u != "":
		return func() tea.Msg {
			if err := openURL(u); err != nil {
				return core.Toast{Text: "Could not open the browser. Copy the link instead.", Bad: true}
			}
			return nil
		}
	}
	return nil
}

func (s *SignIn) window(st *core.State, maxW, maxH int) *ui.Window {
	p := ui.P()
	win := &ui.Window{W: min(68, maxW), Title: "Finish in your browser", Sub: "Check that it shows this code.", Buttons: s.buttons()}
	win.Active = win.Index(s.at)
	if win.Active < 0 {
		win.Active = win.Index(s.primary())
	}
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
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Waiting for dokky.ritik.me", p.Muted).Pad(win.Inner()))
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
		body.Put(0, 3, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Waiting for you to approve", p.Muted).Pad(win.Inner()))
		rows = 4
	}
	win.Fit(rows, maxH)
	win.Body = body
	return win
}

func (s *SignIn) View(st *core.State, maxW, maxH int) *lipgloss.Layer {
	return s.window(st, maxW, maxH).Layer()
}

// enter names the focused button; the letters show while their buttons do
func (s *SignIn) Actions(*core.State) []core.Action {
	enter := core.Action{Key: "enter", Label: "cancel"}
	for _, b := range s.buttons() {
		if "btn:"+b.ID == s.at {
			enter = core.Action{Key: "enter", Label: strings.ToLower(b.Label), Accent: b.Primary}
		}
	}
	acts := []core.Action{enter}
	if len(s.buttons()) > 1 {
		acts = append(acts, core.Action{Key: "← →", Label: "choose"})
	}
	if slices.Contains(s.stops(), "btn:open") {
		acts = append(acts, core.Action{Key: "c", Label: "copy link"}, core.Action{Key: "o", Label: "open browser"})
	}
	return append(acts, core.Action{Key: "esc", Label: "cancel"})
}

func (s *SignIn) Animating(*core.State) bool { return s.status == "waiting" && !ui.Reduced() }
