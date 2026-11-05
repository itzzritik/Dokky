package client

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"syscall"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/platform"
)

const (
	quick = 10 * time.Second
	slow  = 2 * time.Minute
	long  = 5 * time.Minute
)

var ErrNotRunning = errors.New("dokky isn't running")

type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

type Client struct{ state string }

type ConfigureParams struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Options map[string]string `json:"options"`
	State   string            `json:"state"`
	Result  string            `json:"result"`
	EditOf  string            `json:"editOf"`
}

type ConfigureReply struct {
	ID   string   `json:"id"`
	Step api.Step `json:"step"`
}

type LoginStart struct {
	UserCode  string `json:"userCode"`
	VerifyURL string `json:"verifyUrl"`
	Interval  int    `json:"interval"`
	Resume    bool   `json:"resume"`
}

type LoginPoll struct {
	Status  string      `json:"status"`
	Clashes []api.Clash `json:"clashes"`
}

type Service struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Options    map[string]string `json:"options"`
	SecretsSet []string          `json:"secretsSet"`
	Version    int64             `json:"version"`
}

type Event struct {
	Kind     string
	State    *api.State
	Activity *api.Activity
	Progress *api.AddProgress
}

func Dial(ctx context.Context) (*Client, error) {
	state, err := platform.StateDir()
	if err != nil {
		return nil, err
	}
	c := &Client{state: state}
	return c, c.call(ctx, quick, api.MState, nil, nil)
}

func (c *Client) call(ctx context.Context, d time.Duration, method string, params, out any) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	conn, err := ipc.Connect(c.state)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return ErrNotRunning
		}
		return err
	}
	defer conn.Close()
	err = conn.Call(ctx, method, params, out)
	var ie *ipc.Error
	if errors.As(err, &ie) {
		return &Error{Code: ie.Code, Message: ie.Message}
	}
	return err
}

func get[T any](c *Client, ctx context.Context, d time.Duration, method string, params any) (T, error) {
	var out T
	err := c.call(ctx, d, method, params, &out)
	return out, err
}

type id struct {
	ID string `json:"id"`
}

func (c *Client) State(ctx context.Context) (api.State, error) {
	return get[api.State](c, ctx, quick, api.MState, nil)
}

func (c *Client) Activity(ctx context.Context) (api.Activity, error) {
	return get[api.Activity](c, ctx, quick, api.MActivity, nil)
}

func (c *Client) Catalog(ctx context.Context) ([]api.Provider, error) {
	return get[[]api.Provider](c, ctx, quick, api.MCatalog, nil)
}

func (c *Client) Accounts(ctx context.Context) ([]api.AccountRef, error) {
	return get[[]api.AccountRef](c, ctx, quick, api.MAccounts, nil)
}

func (c *Client) Options(ctx context.Context, typ string) ([]api.Option, error) {
	ps, err := get[[]api.ProviderOptions](c, ctx, quick, "providers", nil)
	for _, p := range ps {
		if p.Type == typ {
			return p.Options, nil
		}
	}
	if err == nil {
		err = errors.New("unknown service " + typ)
	}
	return nil, err
}

func (c *Client) AddDrive(ctx context.Context, d api.NewDrive) (api.Drive, error) {
	return get[api.Drive](c, ctx, long, api.MDriveAdd, d)
}

func (c *Client) UpdateDrive(ctx context.Context, u api.DriveUpdate) (api.Drive, error) {
	return get[api.Drive](c, ctx, slow, api.MDriveUpdate, u)
}

func (c *Client) RemoveDrive(ctx context.Context, r api.DriveRemove) error {
	return c.call(ctx, slow, api.MDriveRemove, r, nil)
}

func (c *Client) Connect(ctx context.Context, driveID string) (api.Drive, error) {
	return get[api.Drive](c, ctx, slow, api.MDriveConnect, id{driveID})
}

func (c *Client) Disconnect(ctx context.Context, driveID string) (api.Drive, error) {
	return get[api.Drive](c, ctx, slow, api.MDriveDisconnect, id{driveID})
}

func (c *Client) Retry(ctx context.Context, driveID, name string) error {
	return c.call(ctx, quick, api.MTransferRetry, map[string]string{"id": driveID, "name": name}, nil)
}

func (c *Client) Configure(ctx context.Context, p ConfigureParams) (ConfigureReply, error) {
	return get[ConfigureReply](c, ctx, long, "service.configure", p)
}

func (c *Client) OAuthURL(ctx context.Context) (string, error) {
	r, err := get[struct {
		AuthURL string `json:"authUrl"`
	}](c, ctx, quick, "oauth.status", nil)
	return r.AuthURL, err
}

func (c *Client) Service(ctx context.Context, serviceID string) (Service, error) {
	return get[Service](c, ctx, quick, "service.get", id{serviceID})
}

// a blank secret keeps the saved one
func (c *Client) PutService(ctx context.Context, s Service, options map[string]string) error {
	p := struct {
		ID      string            `json:"id"`
		Name    string            `json:"name"`
		Type    string            `json:"type"`
		Options map[string]string `json:"options"`
		Version int64             `json:"version"`
		EditOf  string            `json:"editOf"`
	}{s.ID, s.Name, s.Type, options, s.Version, s.ID}
	return c.call(ctx, slow, "service.put", p, nil)
}

func (c *Client) LoginStart(ctx context.Context) (LoginStart, error) {
	return get[LoginStart](c, ctx, slow, "login.start", nil)
}

func (c *Client) LoginPoll(ctx context.Context) (LoginPoll, error) {
	return get[LoginPoll](c, ctx, slow, "login.poll", nil)
}

func (c *Client) LoginResolve(ctx context.Context, kind, name, keep string) ([]api.Clash, error) {
	r, err := get[LoginPoll](c, ctx, slow, "login.resolve", map[string]string{"kind": kind, "name": name, "keep": keep})
	return r.Clashes, err
}

func (c *Client) Logout(ctx context.Context, force bool) (string, error) {
	r, err := get[struct {
		Warning string `json:"warning"`
	}](c, ctx, long, "logout", map[string]bool{"force": force})
	return r.Warning, err
}

func (c *Client) SyncNow(ctx context.Context) error {
	return c.call(ctx, slow, "sync.now", nil, nil)
}

func (c *Client) Devices(ctx context.Context) ([]api.Device, error) {
	return get[[]api.Device](c, ctx, quick, "devices.list", nil)
}

func (c *Client) RenameDevice(ctx context.Context, deviceID, name string) error {
	return c.call(ctx, quick, "devices.rename", map[string]string{"id": deviceID, "name": name}, nil)
}

func (c *Client) RevokeDevice(ctx context.Context, deviceID string) error {
	return c.call(ctx, slow, "devices.revoke", id{deviceID}, nil)
}

func (c *Client) SetSettings(ctx context.Context, s api.Settings) error {
	return c.call(ctx, quick, "settings.set", s, nil)
}

func (c *Client) Events(ctx context.Context) (<-chan Event, error) {
	conn, err := ipc.Connect(c.state)
	if err != nil {
		return nil, ErrNotRunning
	}
	defer conn.Close()
	in, err := conn.Events(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 16)
	go func() {
		defer close(out)
		for ev := range in {
			e := Event{Kind: ev.Event}
			var target any
			switch ev.Event {
			case api.EState:
				e.State = &api.State{}
				target = e.State
			case api.EActivity:
				e.Activity = &api.Activity{}
				target = e.Activity
			case api.EAddProgress:
				e.Progress = &api.AddProgress{}
				target = e.Progress
			default:
				continue
			}
			if json.Unmarshal(ev.Data, target) != nil {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
