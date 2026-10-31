package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/obscure"
)

var Featured = []string{"drive", "smb", "webdav", "sftp", "onedrive", "dropbox", "mega"}

var testRemoteTimeout = time.Minute

const jobPoll = 100 * time.Millisecond

type Option struct {
	Name      string    `json:"name"`
	Help      string    `json:"help"`
	Type      string    `json:"type"`
	Default   string    `json:"default"`
	Required  bool      `json:"required"`
	Advanced  bool      `json:"advanced"`
	Secret    bool      `json:"secret"`
	Hide      bool      `json:"hide"`
	Exclusive bool      `json:"exclusive"`
	Examples  []Example `json:"examples"`
}

type Example struct {
	Value string `json:"value"`
	Help  string `json:"help"`
}

type Provider struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Options     []Option `json:"options"`
}

type Step struct {
	State   string            `json:"state"`
	Option  *Option           `json:"option"`
	Error   string            `json:"error"`
	Done    bool              `json:"done"`
	Service map[string]string `json:"service"`
}

type rcOption struct {
	Name, Help, DefaultStr, Type                         string
	Required, Advanced, IsPassword, Sensitive, Exclusive bool
	Hide                                                 fs.OptionVisibility
	Examples                                             []Example
}

func (o rcOption) option() Option {
	opt := Option{
		Name:      o.Name,
		Help:      o.Help,
		Type:      o.Type,
		Default:   o.DefaultStr,
		Required:  o.Required,
		Advanced:  o.Advanced,
		Secret:    o.IsPassword || o.Sensitive,
		Hide:      o.Hide&fs.OptionHideConfigurator != 0,
		Exclusive: o.Exclusive,
		Examples:  o.Examples,
	}
	if opt.Default == "<nil>" {
		opt.Default = ""
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

func OAuthURL() (string, error) {
	var out struct{ AuthURL string }
	err := rpc("config/oauthstatus", nil, &out)
	return out.AuthURL, err
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

func (e *Engine) ConfigStep(ctx context.Context, name, typ string, opts map[string]string, state, result string) (Step, error) {
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
	if err := runJob(ctx, "config/create", in, &out); err != nil {
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
	return e.Reachable(s, "")
}

func (e *Engine) Reachable(s store.Service, remotePath string) error {
	e.opMu.Lock()
	e.clearFsCache(s.ID)
	e.opMu.Unlock()
	_, err := e.ListDirs(s, remotePath)
	return err
}

func (e *Engine) ListDirs(s store.Service, remotePath string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testRemoteTimeout)
	defer cancel()
	var out struct{ List []struct{ Name string } }
	err := runJob(ctx, "operations/list", probeParams(s.ID, remotePath), &out)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("%s did not answer within %s", s.Name, testRemoteTimeout)
	}
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(out.List))
	for _, d := range out.List {
		dirs = append(dirs, d.Name)
	}
	return dirs, nil
}

// librclone.RPC has no context, so a job is the only call that can be stopped
func runJob(ctx context.Context, method string, in map[string]any, out any) error {
	in["_async"] = true
	var job struct{ JobID int64 }
	if err := rpc(method, in, &job); err != nil {
		return err
	}
	for {
		var st struct {
			Finished bool
			Error    string
			Output   json.RawMessage
		}
		if err := rpc("job/status", map[string]any{"jobid": job.JobID}, &st); err != nil {
			return err
		}
		if st.Finished {
			if st.Error != "" {
				return errors.New(st.Error)
			}
			return json.Unmarshal(st.Output, out)
		}
		select {
		case <-ctx.Done():
			_ = rpc("job/stop", map[string]any{"jobid": job.JobID}, nil)
			return ctx.Err()
		case <-time.After(jobPoll):
		}
	}
}

// without these, listing reads each object's ModTime, one HEAD per object on S3
func probeParams(serviceID, remotePath string) map[string]any {
	return map[string]any{
		"fs": serviceID + ":", "remote": remotePath,
		"opt": map[string]any{"noModTime": true, "noMimeType": true, "dirsOnly": true},
	}
}
