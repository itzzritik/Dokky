package tui

import (
	"fmt"
	"path"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/itzzritik/dokky/cli/internal/store"
)

type addStep int

const (
	addPick addStep = iota
	addBrowse
	addName
	addMountNow
)

type addMount struct {
	step    addStep
	cursor  int
	svc     serviceItem
	dir     string
	dirs    []string
	loading bool
	name    textinput.Model
	err     string
	saved   string
}

type browseMsg struct {
	dir  string
	dirs []string
	err  error
}

type mountSavedMsg struct{ name string }

type browseParams struct {
	ServiceID string `json:"serviceId"`
	Path      string `json:"path"`
}

func (a *app) addKey(k tea.KeyPressMsg) tea.Cmd {
	f := a.home.add
	if k.String() == "esc" {
		if f.step == addName {
			f.step, f.err = addBrowse, ""
		} else {
			a.home.add = nil
		}
		return nil
	}
	switch f.step {
	case addPick:
		switch k.String() {
		case "up", "k":
			f.cursor = clamp(f.cursor-1, len(a.services))
		case "down", "j":
			f.cursor = clamp(f.cursor+1, len(a.services))
		case "enter":
			if len(a.services) == 0 {
				return nil
			}
			f.svc, f.step = a.services[clamp(f.cursor, len(a.services))], addBrowse
			return a.browse("")
		}
	case addBrowse:
		if f.loading {
			return nil
		}
		switch k.String() {
		case "up", "k":
			f.cursor = clamp(f.cursor-1, len(f.dirs)+1)
		case "down", "j":
			f.cursor = clamp(f.cursor+1, len(f.dirs)+1)
		case "backspace", "left":
			if f.dir != "" {
				return a.browse(strings.TrimPrefix(path.Dir(f.dir), "."))
			}
		case "enter", "right":
			if f.cursor > 0 {
				return a.browse(path.Join(f.dir, f.dirs[f.cursor-1]))
			}
			if k.String() == "enter" {
				return a.nameStep()
			}
		}
	case addName:
		if k.String() != "enter" {
			var cmd tea.Cmd
			f.name, cmd = f.name.Update(k)
			return cmd
		}
		name := strings.TrimSpace(f.name.Value())
		f.err = ""
		m := store.Mount{Name: name, ServiceID: f.svc.ID, RemotePath: f.dir, VFS: store.DefaultVFS()}
		return a.do("mount.put", m, func() tea.Msg { return mountSavedMsg{name} })
	case addMountNow:
		switch k.String() {
		case "y":
			a.home.add = nil
			return a.toggleMount(f.saved, true)
		case "n":
			a.home.add = nil
		}
	}
	return nil
}

func (a *app) browse(dir string) tea.Cmd {
	f := a.home.add
	f.loading, f.err = true, ""
	params := browseParams{ServiceID: f.svc.ID, Path: dir}
	return func() tea.Msg {
		ctx, cancel := a.callCtx()
		defer cancel()
		var dirs []string
		err := a.d.Call(ctx, "browse", params, &dirs)
		return browseMsg{dir: dir, dirs: dirs, err: err}
	}
}

func (a *app) nameStep() tea.Cmd {
	f := a.home.add
	f.step = addName
	f.name = textinput.New()
	f.name.Placeholder = "lowercase letters, digits and hyphens"
	name := f.svc.Name
	if f.dir != "" {
		name = path.Base(f.dir)
	}
	f.name.SetValue(slug(name))
	return f.name.Focus()
}

func (a *app) addMsg(msg tea.Msg) tea.Cmd {
	f := a.home.add
	switch msg := msg.(type) {
	case browseMsg:
		f.loading = false
		if msg.err != nil {
			f.err = msg.err.Error()
			return nil
		}
		f.dir, f.dirs, f.cursor = msg.dir, msg.dirs, 0
	case mountSavedMsg:
		f.step, f.saved = addMountNow, msg.name
		return a.refreshStatus()
	default:
		if f.step == addName {
			var cmd tea.Cmd
			f.name, cmd = f.name.Update(msg)
			return cmd
		}
	}
	return nil
}

func (a *app) addView() (string, string) {
	f := a.home.add
	var b strings.Builder
	b.WriteString(bold.Render("Add a mount") + "\n\n")
	var help string
	switch f.step {
	case addPick:
		if len(a.services) == 0 {
			b.WriteString("No services yet. Add one on the Services tab first.\n")
			return b.String(), "esc back"
		}
		b.WriteString("Pick a service:\n")
		cur := clamp(f.cursor, len(a.services))
		for i, s := range a.services {
			fmt.Fprintf(&b, "%s%s  %s\n", cursor(i == cur), s.Name, faint.Render(s.Type))
		}
		help = "enter pick · esc cancel"
	case addBrowse:
		fmt.Fprintf(&b, "%s: /%s\n\n", f.svc.Name, f.dir)
		if f.loading {
			b.WriteString("Loading folders...\n")
			break
		}
		cur := clamp(f.cursor, len(f.dirs)+1)
		b.WriteString(cursor(cur == 0) + bold.Render("[ use this folder ]") + "\n")
		for i, d := range f.dirs {
			b.WriteString(cursor(cur == i+1) + d + "/\n")
		}
		help = "enter open or pick · backspace up · esc cancel"
	case addName:
		fmt.Fprintf(&b, "Folder: %s:/%s\n\nName for this mount:\n%s\n", f.svc.Name, f.dir, f.name.View())
		help = "enter save · esc back"
	case addMountNow:
		fmt.Fprintf(&b, "Saved %s. Mount %s now? y/n\n", f.saved, f.saved)
		help = "y mount now · n later"
	}
	if f.err != "" {
		b.WriteString("\n" + red.Render(f.err) + "\n")
	}
	return b.String(), help
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
