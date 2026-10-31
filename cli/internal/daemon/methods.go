package daemon

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/itzzritik/dokky/cli/internal/engine"
	"github.com/itzzritik/dokky/cli/internal/store"
)

const configureTimeout = 5 * time.Minute

type configureParams struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Options map[string]string `json:"options"`
	State   string            `json:"state"`
	Result  string            `json:"result"`
}

type configureReply struct {
	ID   string      `json:"id"`
	Step engine.Step `json:"step"`
}

type testParams struct {
	Service store.Service `json:"service"`
}

type testReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type browseParams struct {
	ServiceID string `json:"serviceId"`
	Path      string `json:"path"`
}

type transfer struct {
	Mount string             `json:"mount"`
	Items []engine.QueueItem `json:"items"`
}

func (d *Daemon) configure(ctx context.Context, p configureParams) (configureReply, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	} else if _, saved := d.findService(p.ID); saved {
		// rclone wipes the section it configures, so an edit runs under a fresh id
		return configureReply{}, fmt.Errorf("service %q is saved, configure changes under a new id", p.ID)
	}
	ctx, cancel := context.WithTimeout(ctx, configureTimeout)
	defer cancel()
	step, err := d.eng.ConfigStep(ctx, p.ID, p.Type, p.Options, p.State, p.Result)
	if err != nil {
		d.eng.DeleteService(p.ID)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return configureReply{}, fmt.Errorf("setup did not finish within %s", configureTimeout)
		}
		return configureReply{}, err
	}
	return configureReply{ID: p.ID, Step: step}, nil
}

// a throwaway id keeps the live section of a saved service untouched
func (d *Daemon) testService(s store.Service) testReply {
	s.ID = uuid.NewString()
	defer d.eng.DeleteService(s.ID)
	if err := d.eng.TestRemote(s); err != nil {
		return testReply{Error: err.Error()}
	}
	return testReply{OK: true}
}

func (d *Daemon) browse(p browseParams) ([]string, error) {
	s, ok := d.findService(p.ServiceID)
	if !ok {
		return nil, fmt.Errorf("no service with id %q", p.ServiceID)
	}
	return d.eng.ListDirs(s, p.Path)
}

func (d *Daemon) transfers() []transfer {
	type live struct{ mount, engineName string }
	var mounted []live
	d.mu.Lock()
	for _, m := range d.data.Mounts {
		if st := d.states[m.ID]; st != nil && st.state == stateMounted {
			mounted = append(mounted, live{m.Name, st.name})
		}
	}
	d.mu.Unlock()
	out := []transfer{}
	for _, l := range mounted {
		// an error means it went away since; the next refresh marks it unmounted
		if items, err := d.eng.Queue(l.engineName); err == nil {
			out = append(out, transfer{Mount: l.mount, Items: items})
		}
	}
	return out
}

func (d *Daemon) settings() store.Settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Settings
}

func (d *Daemon) setSettings(s store.Settings) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data.Settings = s
	return d.save()
}

func splitSecrets(s store.Service) (store.Service, error) {
	ps, err := engine.Providers()
	if err != nil {
		return s, err
	}
	i := slices.IndexFunc(ps, func(p engine.Provider) bool { return p.Type == s.Type })
	if i < 0 {
		return s, fmt.Errorf("unknown service type %q", s.Type)
	}
	all := map[string]string{}
	maps.Copy(all, s.Options)
	maps.Copy(all, s.Secrets)
	s.Options, s.Secrets = engine.SplitSecrets(ps[i], all)
	return s, nil
}

func (d *Daemon) findService(id string) (store.Service, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := slices.IndexFunc(d.data.Services, func(s store.Service) bool { return s.ID == id })
	if i < 0 {
		return store.Service{}, false
	}
	return d.data.Services[i], true
}
