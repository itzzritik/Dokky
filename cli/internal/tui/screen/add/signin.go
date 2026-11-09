package add

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/client"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/tui/core"
	"github.com/itzzritik/dokky/cli/internal/tui/ui"
)

var (
	labels = map[string]string{
		"host": "Server", "user": "Username", "pass": "Password", "port": "Port", "url": "URL", "key_file": "Key file",
		"key_file_pass": "Key password", "access_key_id": "Access key", "secret_access_key": "Secret key", "bearer_token": "Token",
	}
	sftpFields = []string{"host", "user", "port", "_auth", "pass", "key_file", "key_file_pass"}
)

const authField = "_auth"

type field struct {
	opt   api.Option
	in    *ui.Field
	chips []string
	vals  []string
	sel   int
	kept  bool
}

func label(o api.Option) string {
	if l, ok := labels[o.Name]; ok {
		return l
	}
	s := strings.ReplaceAll(o.Name, "_", " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

func newField(o api.Option, base *client.Service) *field {
	v := o.Default
	if base != nil {
		if x, ok := base.Options[o.Name]; ok {
			v = x
		}
	}
	f := &field{opt: o}
	switch {
	case o.Type == "bool":
		f.chips, f.vals = []string{"No", "Yes"}, []string{"false", "true"}
	case o.Exclusive && len(o.Examples) > 0 && len(o.Examples) <= 4:
		for _, e := range o.Examples {
			f.chips, f.vals = append(f.chips, cmpOr(e.Value, "None")), append(f.vals, e.Value)
		}
	}
	if f.chips != nil {
		f.sel = max(0, slices.Index(f.vals, v))
		return f
	}
	hint := ""
	var ex []string
	for _, e := range o.Examples[:min(3, len(o.Examples))] {
		if e.Value != "" {
			ex = append(ex, e.Value)
		}
	}
	if len(ex) > 0 {
		hint = "Like " + strings.Join(ex, ", ")
	}
	if o.Secret && base != nil && slices.Contains(base.SecretsSet, o.Name) {
		v, hint, f.kept = "", "Saved, leave blank to keep", true
	}
	f.in = ui.NewField(label(o), v, hint, o.Secret)
	return f
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (f *field) value() string {
	if f.chips != nil {
		return f.vals[f.sel]
	}
	return f.in.Value()
}

type signForm struct {
	typ, editOf string
	base        *client.Service
	fields      []*field
	at          string // the focused stop
	oauth       bool

	cfgID, qState, qErr string
	question            *field
	authURL             string
	err                 string
	draft               map[string]string

	loadReq, req, authPoll, saveReq core.Req
	cancel                          context.CancelFunc
}

type loaded struct {
	opts []api.Option
	base *client.Service
}

type authTick struct{ req core.Req }

func (s *signForm) load(st *core.State, typ, editOf string) tea.Cmd {
	s.stop()
	*s = signForm{typ: typ, editOf: editOf, loadReq: core.NextReq()}
	return core.Do(st, s.loadReq, func(ctx context.Context, c *client.Client) (any, error) {
		opts, err := c.Options(ctx, typ)
		if err != nil || editOf == "" {
			return loaded{opts: opts}, err
		}
		base, err := c.Service(ctx, editOf)
		return loaded{opts, &base}, err
	})
}

func (s *signForm) working() bool { return s.loadReq != 0 || s.req != 0 || s.saveReq != 0 }

// the browser is open on the provider's sign-in page
func (s *signForm) waiting() bool { return s.oauth && s.req != 0 && s.question == nil }

// a browser sign-in that already finished, kept when Details goes back a step
func (s *signForm) signedIn() bool { return s.oauth && s.question == nil && s.draft != nil }

// a text field takes typing; a chip row takes left, right and space
func stopID(i int, f *field) string {
	if f.chips != nil {
		return "chip:" + strconv.Itoa(i)
	}
	return "field:" + strconv.Itoa(i)
}

// which visible field a stop is, -1 for a button
func fieldIndex(at string) int {
	for _, pre := range []string{"field:", "chip:"} {
		if r, ok := strings.CutPrefix(at, pre); ok {
			if i, err := strconv.Atoi(r); err == nil {
				return i
			}
		}
	}
	return -1
}

// the fields while the form is up; a browser sign-in has none, only its bar
func (s *signForm) stops() []string {
	if s.loadReq != 0 || s.saveReq != 0 || s.oauth && s.question == nil {
		return nil
	}
	var out []string
	for i, f := range s.visible() {
		out = append(out, stopID(i, f))
	}
	return out
}

// the next required field still empty after field i, wrapping round; -1 once they are all filled
func (s *signForm) nextEmpty(i int) int {
	fields := s.visible()
	if i < 0 {
		return -1
	}
	for k := 1; k < len(fields); k++ {
		j := (i + k) % len(fields)
		if f := fields[j]; f.opt.Required && f.value() == "" && !f.kept {
			return j
		}
	}
	return -1
}

// left and right step along a chip row without wrapping; space moves on and wraps, so two chips toggle
func slide(sel *int, n int, k string) bool {
	switch k {
	case "left":
		*sel = max(0, *sel-1)
	case "right":
		*sel = min(n-1, *sel+1)
	case "space":
		*sel = (*sel + 1) % n
	default:
		return false
	}
	return true
}

func (s *signForm) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.req = 0
}

func (s *signForm) build(opts []api.Option) {
	var req, basic []*field
	for _, o := range opts {
		if o.Name == "token" {
			s.oauth = true
		}
		if o.Hide || o.Advanced || o.Name == "token" || (s.typ == "sftp" && !slices.Contains(sftpFields, o.Name)) {
			continue
		}
		if o.Required {
			req = append(req, newField(o, s.base))
		} else {
			basic = append(basic, newField(o, s.base))
		}
	}
	s.fields = append(req, basic...)
	if s.oauth {
		s.fields = nil
	}
	if s.typ == "sftp" {
		auth := &field{opt: api.Option{Name: authField}, chips: []string{"Password", "SSH key"}, vals: []string{"", ""}}
		if s.base != nil && s.base.Options["key_file"] != "" {
			auth.sel = 1
		}
		s.fields = append(s.fields, auth)
		slices.SortStableFunc(s.fields, func(a, b *field) int {
			return slices.Index(sftpFields, a.opt.Name) - slices.Index(sftpFields, b.opt.Name)
		})
	}
}

func (s *signForm) visible() []*field {
	if s.question != nil {
		return []*field{s.question}
	}
	key := slices.ContainsFunc(s.fields, func(f *field) bool { return f.opt.Name == authField && f.sel == 1 })
	var out []*field
	for _, f := range s.fields {
		switch f.opt.Name {
		case "pass":
			if key {
				continue
			}
		case "key_file", "key_file_pass":
			if !key {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

func (s *signForm) options() map[string]string {
	out := map[string]string{}
	for _, f := range s.visible() {
		if v := f.value(); f.opt.Name != authField && (v != "" || f.chips != nil) && !(f.kept && v == "") {
			out[f.opt.Name] = v
		}
	}
	return out
}

func (s *signForm) configure(st *core.State, opts map[string]string, state, result string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.req, s.err, s.qErr = cancel, core.NextReq(), "", ""
	p := client.ConfigureParams{ID: s.cfgID, Type: s.typ, Options: opts, State: state, Result: result, EditOf: s.editOf}
	cmd := core.DoCtx(ctx, st, s.req, func(ctx context.Context, c *client.Client) (any, error) { return c.Configure(ctx, p) })
	if s.oauth && s.authURL == "" {
		return tea.Batch(cmd, authAfter(s.req))
	}
	return cmd
}

func authAfter(req core.Req) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return authTick{req} })
}

func (s *signForm) submit(st *core.State) tea.Cmd {
	if q := s.question; q != nil {
		return s.configure(st, nil, s.qState, q.value())
	}
	for i, f := range s.visible() {
		if f.opt.Required && f.value() == "" && !f.kept {
			s.at, s.err = stopID(i, f), "Fill in "+label(f.opt)+"."
			return nil
		}
	}
	return s.configure(st, s.options(), "", "")
}

// Next on the sign-in step: a browser sign-in already done goes straight on, anything else signs in
func (a *Add) signNext(st *core.State) tea.Cmd {
	switch s := &a.sign; {
	case s.working():
		return nil
	case s.signedIn():
		a.goTo(name)
		return nil
	}
	return a.sign.submit(st)
}

func (a *Add) updateSign(msg tea.Msg, st *core.State) tea.Cmd {
	s := &a.sign
	fields := s.visible()
	switch m := msg.(type) {
	case authTick:
		if m.req == s.req && s.authURL == "" {
			s.authPoll = core.NextReq()
			poll := core.Do(st, s.authPoll, func(ctx context.Context, c *client.Client) (any, error) { return c.OAuthURL(ctx) })
			return tea.Batch(poll, authAfter(s.req))
		}
		return nil
	case core.Reply:
		return a.signReply(m, st)
	case core.Click:
		if s.working() {
			return nil
		}
		if i := fieldIndex(m.ID); i >= 0 && i < len(fields) && m.ID == stopID(i, fields[i]) {
			s.at = m.ID
		}
		if rest, ok := strings.CutPrefix(m.ID, "chip:"); ok {
			row, k, _ := strings.Cut(rest, ":")
			if i, err := strconv.Atoi(row); err == nil && i < len(fields) && fields[i].chips != nil && k != "" {
				s.at, fields[i].sel = "chip:"+row, int(k[0]-'0')
			}
		}
		return nil
	case tea.KeyPressMsg:
		return a.keySign(m, st)
	}
	if i := fieldIndex(s.at); i >= 0 && i < len(fields) && fields[i].in != nil && !s.working() {
		cmd, _ := fields[i].in.Type(msg)
		return cmd
	}
	return nil
}

func (a *Add) keySign(m tea.KeyPressMsg, st *core.State) tea.Cmd {
	s := &a.sign
	fields := s.visible()
	k := m.String()
	at := a.focused(st)
	s.at = at
	i := fieldIndex(at)
	switch {
	// c and o stay as letters for the Copy link and Open browser buttons
	case s.waiting() && (k == "c" || k == "o") && s.authURL != "":
		id := map[string]string{"c": "copy", "o": "open"}[k]
		s.at = "btn:" + id
		return s.copyOrOpen(id)
	case s.working() && !s.waiting():
		return nil
	case ui.Pressed(k, at):
		return a.press(strings.TrimPrefix(at, "btn:"), st)
	case k == "enter" && i >= 0:
		if j := s.nextEmpty(i); j >= 0 {
			s.at = stopID(j, fields[j])
			return nil
		}
		return a.signNext(st)
	case i >= 0 && i < len(fields) && fields[i].chips != nil && slide(&fields[i].sel, len(fields[i].chips), k):
		return nil
	case ui.Walk(a.stops(st), &s.at, k):
		return nil
	}
	if i >= 0 && i < len(fields) && fields[i].in != nil {
		cmd, _ := fields[i].in.Type(m)
		return cmd
	}
	return nil
}

func (s *signForm) copyOrOpen(id string) tea.Cmd {
	u := s.authURL
	switch {
	case u == "":
	case id == "copy":
		return tea.Batch(tea.SetClipboard(u), core.Say("Link copied"))
	case id == "open":
		return func() tea.Msg {
			if err := platform.OpenURL(u); err != nil {
				return core.Toast{Text: "Could not open the browser. Copy the link instead.", Bad: true}
			}
			return nil
		}
	}
	return nil
}

// Cancel, Copy link and Open browser while the browser signs in; Back and the step's primary otherwise
func (a *Add) signBar(st *core.State) []ui.Btn {
	s := &a.sign
	back := ui.Btn{ID: "back", Label: "Back"}
	switch {
	case s.waiting() && s.authURL != "":
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}, {ID: "copy", Label: "Copy link"}, {ID: "open", Label: "Open browser", Primary: true}}
	case s.waiting():
		return []ui.Btn{{ID: "cancel", Label: "Cancel"}}
	case s.loadReq != 0 || s.saveReq != 0:
		return []ui.Btn{back}
	case s.signedIn():
		return []ui.Btn{back, {ID: "next", Label: "Next", Primary: true}}
	case s.oauth && s.question == nil:
		return []ui.Btn{back, {ID: "next", Label: "Open browser", Primary: true}}
	}
	next := "Sign in"
	if s.question != nil {
		next = "Continue"
	}
	if s.req != 0 {
		next = ui.Spin(st.T) + " Checking"
	}
	return []ui.Btn{back, {ID: "next", Label: next, Primary: true}}
}

func (a *Add) signReply(m core.Reply, st *core.State) tea.Cmd {
	s := &a.sign
	switch m.Req {
	case s.loadReq:
		s.loadReq = 0
		if m.Err != nil {
			s.err = core.Report(m.Err)
			return nil
		}
		l := m.Val.(loaded)
		s.base, s.at = l.base, ""
		s.build(l.opts)
		if s.oauth && s.editOf == "" {
			return s.configure(st, map[string]string{}, "", "")
		}
	case s.authPoll:
		if u, ok := m.Val.(string); ok && m.Err == nil {
			// the link's buttons arrive with it, focus on Open browser
			if u != "" && s.authURL == "" {
				s.at = "btn:open"
			}
			s.authURL = u
		}
	case s.req:
		s.req, s.cancel = 0, nil
		if m.Err != nil {
			if !errors.Is(m.Err, context.Canceled) {
				s.err = core.Report(m.Err)
			}
			return nil
		}
		r := m.Val.(client.ConfigureReply)
		switch {
		case r.Step.Done:
			s.question, s.draft = nil, r.Step.Service
			return a.signedIn(st)
		case r.Step.Option != nil && r.Step.Option.Name == "config_is_local":
			// this computer has a browser, so rclone opens it
			s.cfgID, s.question = r.ID, nil
			return s.configure(st, nil, r.Step.State, "true")
		case r.Step.Option != nil:
			s.cfgID, s.qState, s.qErr = r.ID, r.Step.State, core.Report(errors.New(r.Step.Error))
			s.question, s.at = newField(*r.Step.Option, nil), ""
		default:
			s.err = "Setup stopped without asking anything. Try again."
		}
	case s.saveReq:
		s.saveReq = 0
		if m.Err != nil {
			s.err = core.Report(m.Err)
			return nil
		}
		a.goTo(name)
		return core.Say("Sign-in saved")
	}
	return nil
}

func (a *Add) signedIn(st *core.State) tea.Cmd {
	s := &a.sign
	if a.edit == nil {
		// details already typed stay when signing in again after going back
		if a.named.name == nil {
			a.named.reset(unique(st, a.prov.Name))
		}
		a.goTo(name)
		return nil
	}
	s.saveReq = core.NextReq()
	base, draft := *s.base, s.draft
	return core.Do(st, s.saveReq, func(ctx context.Context, c *client.Client) (any, error) {
		return nil, c.PutService(ctx, base, draft)
	})
}

func (a *Add) viewSign(st *core.State, win *ui.Window, maxH int, at string) int {
	p := ui.P()
	s := &a.sign
	cw := win.Inner()
	body := ui.NewBox()
	win.Title, win.Sub = "Sign in to "+a.prov.Name, "Enter your details."
	if a.edit != nil {
		win.Title = "Edit " + a.edit.Name
		if n := a.edit.AccountDrives; n > 1 {
			win.Sub = "Also used by " + strconv.Itoa(n-1) + " more drives."
		}
	}
	waiting := func(text string) int {
		body.Put(0, 0, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T(text, p.Muted).Pad(cw))
		if s.authURL != "" {
			body.Put(0, 2, win.Text(s.authURL, p.Faint))
			return 3
		}
		return 1
	}
	switch {
	case s.loadReq != 0:
		win.Body = body
		return waiting("Loading")
	case s.oauth && s.req != 0 && s.question == nil:
		win.Title, win.Sub = "Finish in your browser", "Sign in to "+a.prov.Name+" in the window that opened."
		win.Body = body
		return waiting("Waiting for " + a.prov.Name)
	case s.saveReq != 0:
		win.Body = body
		return waiting("Saving")
	}
	if s.signedIn() {
		win.Ok, win.Title, win.Sub = true, "Signed in to "+a.prov.Name, "Go on to the drive details."
		win.Body = body
		return 0
	}
	if s.oauth && s.question == nil {
		win.Sub = "We open your browser to sign in."
		rows := 0
		if s.err != "" {
			body.Put(0, 0, win.Text(s.err, p.Coral))
			rows = 1
		}
		win.Body = body
		return rows
	}
	off := 0
	if q := s.question; q != nil {
		win.Sub = ""
		help := strings.Split(ansi.Wordwrap(strings.Join(strings.Fields(q.opt.Help), " "), cw, ""), "\n")
		for i, l := range help[:min(5, len(help))] {
			body.Put(0, i, win.Text(l, p.Muted))
		}
		off = min(5, len(help)) + 1
		if q.in != nil {
			q.in.Label = ""
		}
	}
	fields := s.visible()
	room := max(1, (win.Room(maxH)-3-off)/2)
	// with a button focused, the fields nearest the bar show
	fi := fieldIndex(at)
	if fi < 0 {
		fi = len(fields) - 1
	}
	top := max(0, min(fi-room+1, len(fields)-room))
	rows := off
	for i := top; i < len(fields) && i < top+room; i++ {
		f, y := fields[i], off+(i-top)*2
		switch {
		case f.chips != nil && s.question != nil:
			body.Add(0, y, ui.Chips("chip:"+strconv.Itoa(i), f.chips, f.sel, at == stopID(i, f), p.Surface))
		case f.chips != nil:
			body.Put(0, y, ui.L(p.Surface).T(chipLabel(f), p.Muted).Pad(ui.LabelW))
			body.Add(ui.LabelW, y, ui.Chips("chip:"+strconv.Itoa(i), f.chips, f.sel, at == stopID(i, f), p.Surface))
		default:
			body.Add(0, y, f.in.View("field:"+strconv.Itoa(i), cw, at == stopID(i, f) && !s.working(), p.Surface))
		}
		rows = y + 1
	}
	if msg := cmpOr(s.qErr, s.err); msg != "" {
		body.Put(0, rows+1, win.Text(msg, p.Coral))
		rows += 2
	}
	if s.req != 0 {
		body.Put(0, rows+1, ui.L(p.Surface).B(ui.Spin(st.T), ui.SpinFg(st.T)).Sp(1).T("Checking", p.Muted).Pad(cw))
		rows += 2
	}
	win.Body = body
	return rows
}

func chipLabel(f *field) string {
	if f.opt.Name == authField {
		return "Sign in with"
	}
	return label(f.opt)
}
