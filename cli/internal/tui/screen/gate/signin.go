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
	stage  *ui.Stage
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
	s := &SignIn{stage: vault.NewStage()}
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
	s := &SignIn{stage: vault.NewStage(), req: core.NextReq(), status: "waiting"}
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
		switch {
		case m.ID == "key:esc":
			return s.press("btn:cancel", st)
		case m.ID == "key:c":
			return s.letter("c", st)
		case slices.Contains(s.stops(), m.ID):
			s.at = m.ID
			return s.press(m.ID, st)
		}
	case tea.KeyPressMsg:
		switch k := m.String(); k {
		case "esc":
			return s.press("btn:cancel", st)
		case "c", "o":
			return s.letter(k, st)
		case "enter", "space":
			return s.press(s.primary(), st)
		}
	}
	return nil
}

// c copies the link and o opens it while there is one
func (s *SignIn) letter(k string, st *core.State) tea.Cmd {
	if s.status != "waiting" || s.start.VerifyURL == "" {
		return nil
	}
	if k == "o" {
		return s.press("btn:open", st)
	}
	return s.press("btn:copy", st)
}

// one button, as Forged's card has: open the link, or try again; esc cancels and c copies
func (s *SignIn) buttons() []ui.Btn {
	switch {
	case s.status != "waiting":
		return []ui.Btn{{ID: "retry", Label: "Try again", Primary: true}}
	case s.start.VerifyURL == "":
		return nil
	}
	return []ui.Btn{{ID: "open", Label: "Open link", Primary: true}}
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

// the one button, else cancel
func (s *SignIn) primary() string {
	if btns := s.buttons(); len(btns) > 0 {
		return "btn:" + btns[0].ID
	}
	return "btn:cancel"
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

// Forged's sign in card: the code on a strip, the wait, the link, c to copy and one button
func (s *SignIn) card(st *core.State, w, maxH int) *ui.Window {
	p := ui.P()
	win := &ui.Window{W: ui.CardWidth(w), Title: "Sign in to dokky", Sub: "Approve this code in your browser.", Buttons: s.buttons(), Active: 0,
		Hints: ui.WindowKeys(s.Actions(st))}
	cw, bg := win.Inner(), p.Window
	body := ui.NewBox()
	rows := 0
	line := func(l string) {
		body.Put(0, rows, l)
		rows++
	}
	switch {
	case s.status == "denied":
		win.Title, win.Sub, win.Bad = "The sign-in was denied", "Press enter to try again.", true
	case s.status == "expired":
		win.Title, win.Sub, win.Bad = "The code expired", "Press enter to get a new one.", true
	case s.status == "failed":
		win.Title, win.Sub, win.Bad = "Could not sign in", "Press enter to try again.", true
		for _, l := range ui.Wrap(s.err, cw) {
			line(ui.L(bg).T(l, p.Coral).Pad(cw))
		}
	case s.start.UserCode == "":
		win.Sub = ""
		line(ui.L(bg).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Waiting for dokky.ritik.me", p.Muted).Pad(cw))
	default:
		code := strings.Join(strings.Split(s.start.UserCode, ""), " ")
		left := max(0, (cw-ui.Width(code))/2)
		line(ui.L(p.Sel).Sp(left).B(code, p.Text).Pad(cw))
		rows++
		line(ui.L(bg).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Waiting for approval", p.Muted).Pad(cw))
		for _, l := range ui.Wrap(s.start.VerifyURL, cw) {
			line(ui.L(bg).T(l, p.Accent).Pad(cw))
		}
	}
	win.Body = body
	win.Fit(rows, maxH)
	return win
}

func (s *SignIn) View(st *core.State, w, h int) *lipgloss.Layer {
	return s.stage.Layer(w, h, st.Now, func(maxH int) *ui.Window { return s.card(st, w, maxH) })
}

// the gate's card covers the screen
func (s *SignIn) Full() bool { return true }

// enter presses the one button; c copies the link; esc cancels
func (s *SignIn) Actions(*core.State) []core.Action {
	enter := core.Action{Key: "enter", Label: "Cancel"}
	if btns := s.buttons(); len(btns) > 0 {
		enter.Label = btns[0].Label
	}
	acts := []core.Action{enter}
	if s.status == "waiting" && s.start.VerifyURL != "" {
		acts = append(acts, core.Action{Key: "c", Label: "Copy link"})
	}
	return append(acts, core.Action{Key: "esc", Label: "Cancel"})
}

func (s *SignIn) Animating(*core.State) bool {
	return s.status == "waiting" && !ui.Reduced() || s.stage.Moving()
}
