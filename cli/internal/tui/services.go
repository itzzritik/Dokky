package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
)

type svcMode int

const (
	svcList svcMode = iota
	svcLoading
	svcPickType
	svcForm
	svcWorking
	svcQuestion
	svcFailed
	svcConfirmDelete
)

type services struct {
	cursor    int
	mode      svcMode
	providers []engine.Provider
	form      *huh.Form
	flow      *flow
	deleting  serviceItem
	gen       int
}

type flow struct {
	gen      int
	editID   string
	base     *serviceInfo
	name     string
	typ      string
	provider engine.Provider
	fields   []*field
	advanced bool
	id       string
	step     engine.Step
	question *field
	config   map[string]string
	busy     string
	authURL  string
	err      string
	cancel   context.CancelFunc
	secrets  []string
}

type field struct {
	opt  engine.Option
	s    string
	b    bool
	kept bool
}

type serviceInfo struct {
	Options    map[string]string `json:"options"`
	SecretsSet []string          `json:"secretsSet"`
	Version    int64             `json:"version"`
}

type (
	providersMsg []engine.Provider
	baseMsg      struct {
		gen  int
		info serviceInfo
	}
	configureMsg struct {
		gen  int
		id   string
		step engine.Step
		err  error
	}
	oauthMsg struct {
		gen int
		url string
	}
	testedMsg struct {
		gen int
		err error
	}
	savedMsg struct {
		gen int
		err error
	}
)

func (a *app) svcKey(k tea.KeyPressMsg) tea.Cmd {
	s := &a.svc
	switch s.mode {
	case svcList:
		n := len(a.services)
		switch k.String() {
		case "up", "k":
			s.cursor = clamp(s.cursor-1, n)
		case "down", "j":
			s.cursor = clamp(s.cursor+1, n)
		case "a":
			return a.startFlow(&flow{})
		case "e":
			if n > 0 {
				it := a.services[clamp(s.cursor, n)]
				return a.startFlow(&flow{editID: it.ID, name: it.Name, typ: it.Type})
			}
		case "d":
			if n > 0 {
				s.deleting, s.mode = a.services[clamp(s.cursor, n)], svcConfirmDelete
			}
		}
	case svcConfirmDelete:
		switch k.String() {
		case "y":
			s.mode = svcList
			return a.do("service.delete", map[string]string{"id": s.deleting.ID}, tea.Batch(a.fetchServices(), a.refreshStatus()))
		case "n", "esc":
			s.mode = svcList
		}
	case svcPickType, svcForm, svcQuestion:
		if k.String() == "esc" && !filtering(s.form) {
			a.endFlow()
			return nil
		}
		return a.updateForm(k)
	case svcLoading, svcWorking:
		if k.String() == "esc" {
			a.endFlow()
		}
	case svcFailed:
		switch k.String() {
		case "enter":
			return a.openOptions()
		case "esc":
			a.endFlow()
		}
	}
	return nil
}

func (a *app) svcMsg(msg tea.Msg) tea.Cmd {
	s := &a.svc
	f := s.flow
	switch msg := msg.(type) {
	case providersMsg:
		s.providers = msg
		if s.mode == svcLoading && f != nil {
			return a.advance()
		}
	case baseMsg:
		if f != nil && msg.gen == f.gen {
			f.base = &msg.info
			return a.advance()
		}
	case errMsg:
		if s.mode == svcLoading {
			a.endFlow()
		}
	case configureMsg:
		if f == nil || msg.gen != f.gen {
			return nil
		}
		if msg.err != nil {
			return a.fail(msg.err)
		}
		f.id, f.step, f.authURL = msg.id, msg.step, ""
		switch {
		case msg.step.Done:
			f.config = msg.step.Service
			for k, v := range f.config {
				if f.isSecret(k) {
					f.secrets = append(f.secrets, v)
				}
			}
			f.busy = "Testing the connection to " + f.name
			return a.test()
		case msg.step.Option != nil:
			return a.ask(msg.step)
		}
		return a.fail(errors.New("setup stopped without asking anything"))
	case oauthMsg:
		if f != nil && msg.gen == f.gen {
			f.authURL = msg.url
		}
	case testedMsg:
		if f == nil || msg.gen != f.gen {
			return nil
		}
		if msg.err != nil {
			return a.fail(msg.err)
		}
		f.busy = "Saving " + f.name
		return a.save()
	case savedMsg:
		if f == nil || msg.gen != f.gen {
			return nil
		}
		// retrying would send the same stale version, so the edit starts over from the newer copy
		var ie *ipc.Error
		if errors.As(msg.err, &ie) && ie.Code == "conflict" {
			name := f.name
			a.endFlow()
			a.notice = name + " was saved on another device first, so this edit was not saved. The newer copy: " + ie.Message + ". Edit it again to change it."
			return a.do("sync.now", nil, tea.Batch(a.fetchServices(), a.refreshStatus()))
		}
		if msg.err != nil {
			return a.fail(msg.err)
		}
		a.endFlow()
		return tea.Batch(a.fetchServices(), a.refreshStatus())
	default:
		if s.mode == svcPickType || s.mode == svcForm || s.mode == svcQuestion {
			return a.updateForm(msg)
		}
	}
	return nil
}

func (a *app) startFlow(f *flow) tea.Cmd {
	s := &a.svc
	s.gen++
	f.gen, s.flow = s.gen, f
	return a.advance()
}

func (a *app) advance() tea.Cmd {
	s := &a.svc
	f := s.flow
	switch {
	case f.editID != "" && f.base == nil:
		s.mode = svcLoading
		gen := f.gen
		return fetch(a, "service.get", map[string]string{"id": f.editID}, func(info serviceInfo) tea.Msg { return baseMsg{gen: gen, info: info} })
	case s.providers == nil:
		s.mode = svcLoading
		return fetch(a, "providers", nil, func(p []engine.Provider) tea.Msg { return providersMsg(p) })
	}
	return a.afterProviders()
}

func (a *app) afterProviders() tea.Cmd {
	if a.svc.flow.typ != "" {
		return a.openOptions()
	}
	s := &a.svc
	var opts []huh.Option[string]
	for _, p := range featuredFirst(s.providers) {
		label := fmt.Sprintf("%s (%s)", p.Description, p.Type)
		if h := featuredHelp[p.Type]; h != "" {
			label += ": " + h
		}
		opts = append(opts, huh.NewOption(label, p.Type))
	}
	sel := huh.NewSelect[string]().Title("Service type").Description("Type / to search every type").
		Options(opts...).Value(&s.flow.typ).Height(max(a.height-12, 5))
	s.form, s.mode = a.newForm(huh.NewGroup(sel)), svcPickType
	return s.form.Init()
}

var featuredHelp = map[string]string{
	"drive":    "Files in your Google account",
	"smb":      "A shared folder on a NAS or Windows PC",
	"webdav":   "Nextcloud, ownCloud and other WebDAV servers",
	"sftp":     "Any server you can reach over SSH",
	"onedrive": "Files in your Microsoft account",
	"dropbox":  "Files in your Dropbox account",
	"mega":     "Files in your Mega account",
}

const ownClientHelp = "Use your own Google client ID (recommended). rclone's shared Google Drive client ID is being retired during 2026. How to make one: https://rclone.org/drive/#making-your-own-client-id"

func driveOwnClient(o engine.Option) engine.Option {
	if o.Name == "client_id" || o.Name == "client_secret" {
		o.Advanced, o.Required = true, false
		o.Help = ownClientHelp
	}
	return o
}

func featuredFirst(ps []engine.Provider) []engine.Provider {
	out := make([]engine.Provider, 0, len(ps))
	for _, t := range engine.Featured {
		if i := slices.IndexFunc(ps, func(p engine.Provider) bool { return p.Type == t }); i >= 0 {
			out = append(out, ps[i])
		}
	}
	for _, p := range ps {
		if !slices.Contains(engine.Featured, p.Type) {
			out = append(out, p)
		}
	}
	return out
}

func (a *app) openOptions() tea.Cmd {
	s := &a.svc
	f := s.flow
	if f.fields == nil {
		i := slices.IndexFunc(s.providers, func(p engine.Provider) bool { return p.Type == f.typ })
		if i < 0 {
			a.notice = fmt.Sprintf("unknown service type %q", f.typ)
			a.endFlow()
			return nil
		}
		f.provider = s.providers[i]
		for _, o := range f.provider.Options {
			if o.Hide {
				continue
			}
			if f.typ == "drive" {
				o = driveOwnClient(o)
			}
			fl := &field{opt: o, s: o.Default}
			if f.base != nil {
				if v, ok := f.base.Options[o.Name]; ok {
					fl.s = v
				}
				if o.Secret && slices.Contains(f.base.SecretsSet, o.Name) {
					fl.s, fl.kept = "", true
				}
			}
			fl.b = fl.s == "true"
			f.fields = append(f.fields, fl)
		}
	}
	basic := []huh.Field{huh.NewInput().Title("Name").Value(&f.name).Validate(required)}
	var adv []huh.Field
	for _, fl := range f.fields {
		if fl.opt.Required {
			basic = append(basic, fl.input(false))
		}
	}
	for _, fl := range f.fields {
		switch {
		case fl.opt.Required:
		case fl.opt.Advanced:
			adv = append(adv, fl.input(false))
		default:
			basic = append(basic, fl.input(false))
		}
	}
	if len(adv) > 0 {
		basic = append(basic, huh.NewConfirm().Title("Show advanced options?").Value(&f.advanced))
	}
	title := "New " + f.provider.Description + " service"
	if f.editID != "" {
		title = "Edit " + f.name
	}
	groups := []*huh.Group{huh.NewGroup(basic...).Title(title)}
	if len(adv) > 0 {
		groups = append(groups, huh.NewGroup(adv...).Title("Advanced options").WithHideFunc(func() bool { return !f.advanced }))
	}
	s.form, s.mode = a.newForm(groups...), svcForm
	return s.form.Init()
}

func (a *app) updateForm(msg tea.Msg) tea.Cmd {
	s := &a.svc
	m, cmd := s.form.Update(msg)
	s.form = m.(*huh.Form)
	switch s.form.State {
	case huh.StateAborted:
		a.endFlow()
	case huh.StateCompleted:
		f := s.flow
		switch s.mode {
		case svcPickType:
			return a.openOptions()
		case svcForm:
			return a.configure(f.options(), "", "")
		case svcQuestion:
			v := f.question.value()
			if f.question.opt.Secret {
				f.secrets = append(f.secrets, v)
			}
			return a.configure(nil, f.step.State, v)
		}
	}
	return cmd
}

func (a *app) configure(opts map[string]string, state, result string) tea.Cmd {
	s := &a.svc
	f := s.flow
	s.mode, f.busy, f.authURL, f.err = svcWorking, "Setting up "+f.name, "", ""
	ctx, cancel := context.WithCancel(a.ctx)
	f.cancel = cancel
	gen := f.gen
	params := map[string]any{"id": f.id, "type": f.typ, "options": opts, "state": state, "result": result, "editOf": f.editID}
	return func() tea.Msg {
		defer cancel()
		var r struct {
			ID   string      `json:"id"`
			Step engine.Step `json:"step"`
		}
		err := a.d.Call(ctx, "service.configure", params, &r)
		return configureMsg{gen: gen, id: r.ID, step: r.Step, err: err}
	}
}

func (a *app) oauthPoll() tea.Cmd {
	gen := a.svc.flow.gen
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		var r struct {
			AuthURL string `json:"authUrl"`
		}
		if a.d.Call(ctx, "oauth.status", nil, &r) != nil {
			return nil
		}
		return oauthMsg{gen: gen, url: r.AuthURL}
	}
}

func (a *app) ask(step engine.Step) tea.Cmd {
	s := &a.svc
	f := s.flow
	f.question = &field{opt: *step.Option, s: step.Option.Default, b: step.Option.Default == "true"}
	f.err = step.Error
	s.form, s.mode = a.newForm(huh.NewGroup(f.question.input(true))), svcQuestion
	return s.form.Init()
}

func (a *app) test() tea.Cmd {
	f := a.svc.flow
	gen := f.gen
	svc := store.Service{Name: f.name, Type: f.typ, Options: f.config}
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		var r struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		err := a.d.Call(ctx, "service.test", map[string]any{"service": svc, "editOf": f.editID}, &r)
		if err == nil && !r.OK {
			err = errors.New(r.Error)
		}
		return testedMsg{gen: gen, err: err}
	}
}

func (a *app) save() tea.Cmd {
	f := a.svc.flow
	gen := f.gen
	p := struct {
		store.Service
		EditOf string `json:"editOf,omitempty"`
	}{store.Service{ID: cmp.Or(f.editID, f.id), Name: f.name, Type: f.typ, Options: f.config}, f.editID}
	if f.base != nil {
		p.Version = f.base.Version
	}
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		return savedMsg{gen: gen, err: a.d.Call(ctx, "service.put", p, nil)}
	}
}

func (a *app) fail(err error) tea.Cmd {
	f := a.svc.flow
	if f.cancel != nil {
		f.cancel()
	}
	a.svc.mode, f.err = svcFailed, err.Error()
	return nil
}

func (a *app) endFlow() {
	s := &a.svc
	if s.flow != nil && s.flow.cancel != nil {
		s.flow.cancel()
	}
	s.flow, s.form, s.mode = nil, nil, svcList
}

func (a *app) svcView() (string, string) {
	s := &a.svc
	f := s.flow
	var b strings.Builder
	switch s.mode {
	case svcLoading:
		return "Loading...", "esc cancel"
	case svcPickType, svcForm, svcQuestion:
		if s.mode == svcQuestion && f.err != "" {
			b.WriteString(red.Render(logx.Redact(f.err, f.secrets...)) + "\n\n")
		}
		b.WriteString(s.form.View())
		return b.String(), "esc cancel"
	case svcWorking:
		b.WriteString(f.busy + "...\n")
		if f.authURL != "" {
			b.WriteString("\nSign in with the browser window that opened.\nIf your browser didn't open, visit: " + f.authURL + "\n")
		}
		return b.String(), "esc cancel"
	case svcFailed:
		b.WriteString(red.Render("Could not set up "+f.name+": "+logx.Redact(f.err, f.secrets...)) + "\n\nPress enter to change the settings and try again.\n")
		return b.String(), "enter retry · esc cancel"
	case svcConfirmDelete:
		var mounts []string
		for _, m := range a.status.Mounts {
			if m.ServiceID == s.deleting.ID {
				mounts = append(mounts, m.Name)
			}
		}
		fmt.Fprintf(&b, "Delete %s?\n\n", s.deleting.Name)
		if len(mounts) == 0 {
			b.WriteString("No mounts use it.\n")
		} else {
			b.WriteString("These mounts go with it, unmounted first: " + strings.Join(mounts, ", ") + "\n")
		}
		return b.String(), "y delete · n keep"
	}
	if len(a.services) == 0 {
		return "No services yet. Press a to add one.\n", "a add"
	}
	cur := clamp(s.cursor, len(a.services))
	for i, it := range a.services {
		fmt.Fprintf(&b, "%s%s  %s\n", cursor(i == cur), it.Name, faint.Render(it.Type))
	}
	return b.String(), "a add · e edit · d delete"
}

func (f *flow) options() map[string]string {
	out := map[string]string{}
	for _, fl := range f.fields {
		v := fl.value()
		// basic values are kept even when default: rclone's defaults (like the user name) differ per device
		if v != fl.opt.Default || (!fl.opt.Advanced && v != "") {
			out[fl.opt.Name] = v
		}
		if fl.opt.Secret && v != "" {
			f.secrets = append(f.secrets, v)
		}
	}
	return out
}

func (f *flow) isSecret(name string) bool {
	return slices.ContainsFunc(f.provider.Options, func(o engine.Option) bool { return o.Name == name && o.Secret })
}

func (fl *field) value() string {
	if fl.opt.Type == "bool" {
		return strconv.FormatBool(fl.b)
	}
	return fl.s
}

func (fl *field) input(question bool) huh.Field {
	o := fl.opt
	first, rest, _ := strings.Cut(strings.TrimSpace(o.Help), "\n")
	title, desc := strings.ReplaceAll(o.Name, "_", " "), first
	if question {
		title, desc = first, strings.TrimSpace(rest)
	}
	switch {
	case o.Type == "bool":
		return huh.NewConfirm().Title(title).Description(desc).Value(&fl.b)
	case o.Exclusive && len(o.Examples) > 0:
		var opts []huh.Option[string]
		if !slices.ContainsFunc(o.Examples, func(e engine.Example) bool { return e.Value == fl.s }) {
			opts = append(opts, huh.NewOption(cmp.Or(fl.s, "(none)"), fl.s))
		}
		for _, e := range o.Examples {
			label := cmp.Or(e.Value, "(none)")
			if h, _, _ := strings.Cut(e.Help, "\n"); h != "" {
				label += ": " + h
			}
			opts = append(opts, huh.NewOption(label, e.Value))
		}
		return huh.NewSelect[string]().Title(title).Description(desc).Options(opts...).Value(&fl.s)
	}
	if len(o.Examples) > 0 {
		var vals []string
		for _, e := range o.Examples {
			vals = append(vals, e.Value)
		}
		desc = strings.TrimSpace(desc + " Examples: " + strings.Join(vals, ", "))
	}
	if fl.kept {
		desc = strings.TrimSpace(desc + " Saved. Leave blank to keep it.")
	}
	in := huh.NewInput().Title(title).Description(desc).Value(&fl.s)
	if o.Secret {
		in.EchoMode(huh.EchoModePassword)
	}
	if o.Required && o.Default == "" && !fl.kept {
		in.Validate(required)
	}
	return in
}

func required(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("required")
	}
	return nil
}

func filtering(f *huh.Form) bool {
	sel, ok := f.GetFocusedField().(interface{ GetFiltering() bool })
	return ok && sel.GetFiltering()
}
