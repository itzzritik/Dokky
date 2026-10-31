package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/obscure"
)

var Featured = []string{"drive", "smb", "webdav", "sftp", "onedrive", "dropbox", "mega"}

var testRemoteTimeout = 15 * time.Second

type Option struct {
	Name, Help, Default        string
	Required, Advanced, Secret bool
	Examples                   []string
}

type Provider struct {
	Type, Description string
	Options           []Option
}

type Step struct {
	State   string
	Option  *Option
	Error   string
	Done    bool
	Service map[string]string
}

type rcOption struct {
	Name, Help, DefaultStr                    string
	Required, Advanced, IsPassword, Sensitive bool
	Examples                                  []struct{ Value string }
}

func (o rcOption) option() Option {
	opt := Option{
		Name:     o.Name,
		Help:     o.Help,
		Default:  o.DefaultStr,
		Required: o.Required,
		Advanced: o.Advanced,
		Secret:   o.IsPassword || o.Sensitive,
	}
	if opt.Default == "<nil>" {
		opt.Default = ""
	}
	for _, ex := range o.Examples {
		opt.Examples = append(opt.Examples, ex.Value)
	}
	return opt
}

func Providers() ([]Provider, error) {
	var out struct {
		Providers []struct {
			Name, Description string
			Hide              bool
			Options           []rcOption
		}
	}
	if err := rpc("config/providers", nil, &out); err != nil {
		return nil, err
	}
	var ps []Provider
	for _, p := range out.Providers {
		if p.Hide {
			continue
		}
		pr := Provider{Type: p.Name, Description: p.Description}
		for _, o := range p.Options {
			pr.Options = append(pr.Options, o.option())
		}
		ps = append(ps, pr)
	}
	return ps, nil
}

func SplitSecrets(p Provider, opts map[string]string) (plain, secret map[string]string) {
	isSecret := map[string]bool{}
	for _, o := range p.Options {
		if o.Secret {
			isSecret[o.Name] = true
		}
	}
	plain, secret = map[string]string{}, map[string]string{}
	for k, v := range opts {
		if isSecret[k] {
			secret[k] = v
		} else {
			plain[k] = v
		}
	}
	return plain, secret
}

func isPassword(typ, key string) bool {
	ri, err := fs.Find(typ)
	if err != nil {
		return false
	}
	o := ri.Options.Get(key)
	return o != nil && o.IsPassword
}

func (e *Engine) ConfigStep(name, typ string, opts map[string]string, state, result string) (Step, error) {
	in := map[string]any{
		"name":       name,
		"type":       typ,
		"parameters": opts,
		"opt": map[string]any{
			"nonInteractive": true,
			"obscure":        true,
			"continue":       state != "",
			"state":          state,
			"result":         result,
		},
	}
	var out struct {
		State, Error string
		Option       *rcOption
	}
	if err := rpc("config/create", in, &out); err != nil {
		return Step{}, err
	}
	step := Step{State: out.State, Error: out.Error}
	if out.Option != nil {
		o := out.Option.option()
		step.Option = &o
	}
	if out.State != "" {
		return step, nil
	}
	step.Done = true
	step.Service = map[string]string{}
	for _, k := range e.mem.GetKeyList(name) {
		if k == "type" {
			continue
		}
		v, _ := e.mem.GetValue(name, k)
		if isPassword(typ, k) {
			plain, err := obscure.Reveal(v)
			if err != nil {
				return Step{}, fmt.Errorf("%s: %w", k, err)
			}
			v = plain
		}
		step.Service[k] = v
	}
	return step, nil
}

func (e *Engine) TestRemote(s store.Service) error {
	if err := e.PutService(s); err != nil {
		return err
	}
	e.opMu.Lock()
	e.clearFsCache(s.ID)
	e.opMu.Unlock()

	var job struct{ JobID int64 }
	if err := rpc("operations/list", map[string]any{"fs": s.ID + ":", "remote": "", "_async": true}, &job); err != nil {
		return err
	}
	deadline := time.Now().Add(testRemoteTimeout)
	for {
		var st struct {
			Finished bool
			Error    string
		}
		if err := rpc("job/status", map[string]any{"jobid": job.JobID}, &st); err != nil {
			return err
		}
		if st.Finished {
			if st.Error != "" {
				return errors.New(st.Error)
			}
			return nil
		}
		if time.Now().After(deadline) {
			_ = rpc("job/stop", map[string]any{"jobid": job.JobID}, nil)
			return fmt.Errorf("%s did not answer within %s", s.Name, testRemoteTimeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
