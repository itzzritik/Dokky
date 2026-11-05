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
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
)

const configureTimeout = 5 * time.Minute

type configureParams struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Options map[string]string `json:"options"`
	State   string            `json:"state"`
	Result  string            `json:"result"`
	EditOf  string            `json:"editOf"`
}

type configureReply struct {
	ID   string      `json:"id"`
	Step engine.Step `json:"step"`
}

type testParams struct {
	Service store.Service `json:"service"`
	EditOf  string        `json:"editOf"`
}

type putParams struct {
	store.Service
	EditOf string `json:"editOf"`
}

type serviceInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Options    map[string]string `json:"options"`
	SecretsSet []string          `json:"secretsSet"`
	Version    int64             `json:"version"`
}

type testReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type browseParams struct {
	ServiceID string `json:"serviceId"`
	Path      string `json:"path"`
}

type serviceItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type oauthReply struct {
	AuthURL string `json:"authUrl"`
}

func (d *Daemon) configure(ctx context.Context, p configureParams) (configureReply, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	} else if _, saved := d.findService(p.ID); saved {
		// rclone wipes the section it configures, so an edit runs under a fresh id
		return configureReply{}, fmt.Errorf("service %q is saved, configure changes under a new id", p.ID)
	}
	if p.State == "" {
		var kept bool
		if p.Options, kept = d.keepSecrets(p.EditOf, p.Type, p.Options); kept {
			d.mu.Lock()
			d.edits[p.ID] = p.EditOf
			d.mu.Unlock()
		}
	}
	ctx, cancel := context.WithTimeout(ctx, configureTimeout)
	defer cancel()
	step, err := d.eng.ConfigStep(ctx, p.ID, p.Type, p.Options, p.State, p.Result)
	if err != nil || step.Done {
		d.mu.Lock()
		editOf := d.edits[p.ID]
		delete(d.edits, p.ID)
		d.mu.Unlock()
		if saved, ok := d.findService(editOf); ok && err == nil {
			// test and put fill these back from the store, so callers never see saved secrets
			for k, v := range saved.Secrets {
				if step.Service[k] == v {
					step.Service[k] = ""
				}
			}
		}
	}
	if err != nil {
		d.eng.DeleteService(p.ID)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return configureReply{}, fmt.Errorf("setup did not finish within %s", configureTimeout)
		}
		return configureReply{}, err
	}
	return configureReply{ID: p.ID, Step: step}, nil
}

func (d *Daemon) serviceList() []serviceItem {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]serviceItem, 0, len(d.data.Services))
	for _, s := range d.data.Services {
		out = append(out, serviceItem{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return out
}

func (d *Daemon) serviceGet(p idParams) (serviceInfo, error) {
	s, ok := d.findService(p.ID)
	if !ok {
		return serviceInfo{}, fmt.Errorf("no service with id %q", p.ID)
	}
	info := serviceInfo{ID: s.ID, Name: s.Name, Type: s.Type, Options: maps.Clone(s.Options), SecretsSet: []string{}, Version: s.Version}
	for _, k := range slices.Sorted(maps.Keys(s.Secrets)) {
		if s.Secrets[k] != "" {
			info.SecretsSet = append(info.SecretsSet, k)
		}
	}
	return info, nil
}

// an edit leaves a saved secret blank to keep it; only the same type gets them
func (d *Daemon) keepSecrets(editOf, typ string, opts map[string]string) (map[string]string, bool) {
	if editOf == "" {
		return opts, false
	}
	saved, ok := d.findService(editOf)
	if !ok || saved.Type != typ {
		return opts, false
	}
	out := maps.Clone(opts)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range saved.Secrets {
		if out[k] == "" {
			out[k] = v
		}
	}
	return out, true
}

func oauthStatus() (oauthReply, error) {
	u, err := engine.OAuthURL()
	return oauthReply{AuthURL: u}, err
}

// a throwaway id keeps the live section of a saved service untouched
func (d *Daemon) testService(s store.Service) testReply {
	s.ID = uuid.NewString()
	defer d.eng.DeleteService(s.ID)
	if err := d.eng.TestRemote(s); err != nil {
		var extra []string
		if split, serr := splitSecrets(s); serr == nil {
			extra = slices.Collect(maps.Values(split.Secrets))
		}
		return testReply{Error: logx.Redact(err.Error(), extra...)}
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

func (d *Daemon) settings() store.Settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.data.Settings
}

func (d *Daemon) setSettings(s store.Settings) error {
	d.mu.Lock()
	err := writePin(d.upd.paths.State, s.PinnedVersion)
	if err == nil {
		d.data.Settings = s
		err = d.save()
	}
	d.mu.Unlock()
	d.publish()
	return err
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
