package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/itzzritik/dokky/cli/internal/vault"
)

const (
	defaultBase  = "https://dokky.ritik.me"
	maxBodyBytes = 8 << 20
)

var (
	ErrRevoked      = errors.New("device revoked")
	ErrUnauthorized = errors.New("unauthorized")
	ErrNameTaken    = errors.New("name already taken")
	ErrNotFound     = errors.New("not found")
)

// the server removed this device; Mode says whether it finishes its uploads first
type RevokedError struct {
	Mode, Msg string
}

func (e *RevokedError) Error() string      { return ErrRevoked.Error() + ": " + e.Msg }
func (*RevokedError) Is(target error) bool { return target == ErrRevoked }

// an answer without a mode comes from before modes existed, which always erased
func RevokedMode(err error) string {
	var re *RevokedError
	if errors.As(err, &re) && re.Mode == "remove" {
		return "remove"
	}
	return "erase"
}

type ConflictError struct {
	Current json.RawMessage
}

func (*ConflictError) Error() string { return "conflict: saved elsewhere first" }

type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("server returned %d %s: %s", e.Status, e.Code, e.Message)
}

type Client struct {
	Base    string
	Token   string
	Version string
	HTTP    *http.Client
	// set once the vault is unlocked: drive logins are sealed with the account key and every request says so
	Sealer *Sealer
}

type Snapshot struct {
	Rev      string
	Services []store.Service
	Mounts   []store.Mount
	// for a vault-aware pull: the account's vault (nil when it has none) and the services the server still
	// holds with its own key, which this device seals and uploads again
	Vault  *VaultRef
	Legacy []string
}

type DeviceInfo struct {
	DeviceName string `json:"deviceName"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Version    string `json:"version"`
}

type LoginStart struct {
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	VerifyURL  string `json:"verifyUrl"`
	Interval   int    `json:"interval"`
	ExpiresIn  int    `json:"expiresIn"`
}

type LoginPoll struct {
	Status   string `json:"status"`
	Token    string `json:"token"`
	DeviceID string `json:"deviceId"`
}

type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	OSName     string    `json:"osName"`
	Arch       string    `json:"arch"`
	Version    string    `json:"version"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Current    bool      `json:"current"`
	Drives     int       `json:"drives"`
	Mounted    []string  `json:"mounted"`
}

type Me struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, auth bool, headers map[string]string, body any) (*http.Response, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("encode request: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	base := c.Base
	if base == "" {
		base = defaultBase
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/api/v1"+path, rd)
	if err != nil {
		return nil, nil, err
	}
	version := c.Version
	if version == "" {
		version = buildinfo.Version
	}
	req.Header.Set("X-Dokky-Version", version)
	req.Header.Set("X-Dokky-OS", platform.OSName())
	req.Header.Set("X-Dokky-Removal", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Sealer != nil {
		req.Header.Set("X-Dokky-Vault", "1")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if len(data) > maxBodyBytes {
		return nil, nil, errors.New("response too large")
	}
	return resp, data, nil
}

func failure(resp *http.Response, data []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Current json.RawMessage `json:"current"`
		Mode    string          `json:"mode"`
	}
	_ = json.Unmarshal(data, &env)
	code, msg := env.Error.Code, env.Error.Message
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized && code == "device_revoked":
		return &RevokedError{Mode: env.Mode, Msg: msg}
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case resp.StatusCode == http.StatusConflict && code == "conflict":
		return &ConflictError{Current: env.Current}
	case resp.StatusCode == http.StatusConflict && code == "name_taken":
		return fmt.Errorf("%w: %s", ErrNameTaken, msg)
	case resp.StatusCode == http.StatusConflict && code == "key_mismatch":
		return fmt.Errorf("%w: %s", ErrKeyMismatch, msg)
	case resp.StatusCode == http.StatusConflict && code == "vault_exists":
		var cur RemoteVault
		if json.Unmarshal(env.Current, &cur) != nil {
			return &StatusError{Status: resp.StatusCode, Code: code, Message: msg}
		}
		return &VaultExistsError{Current: cur}
	case code == "no_vault":
		return fmt.Errorf("%w: %s", ErrNoVault, msg)
	case resp.StatusCode == http.StatusUpgradeRequired:
		return fmt.Errorf("%w: %s", ErrUpdateRequired, msg)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	}
	return &StatusError{Status: resp.StatusCode, Code: code, Message: msg}
}

func (c *Client) call(ctx context.Context, method, path string, auth bool, body, out any, want int) error {
	if !auth {
		return c.send(ctx, method, path, false, nil, body, out, want)
	}
	return c.callWith(ctx, method, path, nil, body, out, want)
}

func (c *Client) callWith(ctx context.Context, method, path string, headers map[string]string, body, out any, want int) error {
	return c.send(ctx, method, path, true, headers, body, out, want)
}

func (c *Client) send(ctx context.Context, method, path string, auth bool, headers map[string]string, body, out any, want int) error {
	resp, data, err := c.do(ctx, method, path, auth, headers, body)
	if err != nil {
		return err
	}
	if resp.StatusCode != want {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return &StatusError{Status: resp.StatusCode, Code: "unexpected_status", Message: fmt.Sprintf("expected %d", want)}
		}
		return failure(resp, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) LoginStart(ctx context.Context, info DeviceInfo) (LoginStart, error) {
	var out LoginStart
	err := c.call(ctx, http.MethodPost, "/cli/login/start", false, info, &out, http.StatusOK)
	return out, err
}

func (c *Client) LoginPoll(ctx context.Context, deviceCode string) (LoginPoll, error) {
	var out LoginPoll
	err := c.call(ctx, http.MethodPost, "/cli/login/poll", false, map[string]string{"deviceCode": deviceCode}, &out, http.StatusOK)
	return out, err
}

func (c *Client) Pull(ctx context.Context, rev string) (*Snapshot, bool, error) {
	var headers map[string]string
	if rev != "" {
		headers = map[string]string{"If-None-Match": rev}
	}
	resp, data, err := c.do(ctx, http.MethodGet, "/sync", true, headers, nil)
	if err != nil {
		return nil, false, err
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, true, nil
	case http.StatusOK:
	default:
		return nil, false, failure(resp, data)
	}
	etag := resp.Header.Get("ETag")
	if c.Sealer != nil {
		snap, err := c.Sealer.snapshot(data, etag)
		return snap, false, err
	}
	var wire struct {
		Services *[]store.Service `json:"services"`
		Mounts   *[]store.Mount   `json:"mounts"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, false, fmt.Errorf("decode snapshot: %w", err)
	}
	if wire.Services == nil || wire.Mounts == nil || etag == "" {
		return nil, false, errors.New("incomplete snapshot")
	}
	return &Snapshot{Rev: etag, Services: *wire.Services, Mounts: *wire.Mounts}, false, nil
}

// every service opens with this device's key, or the pull fails as a whole: one left out would be deleted here
func (s *Sealer) snapshot(data []byte, etag string) (*Snapshot, error) {
	var wire struct {
		Vault    *VaultRef        `json:"vault"`
		Services *[]sealedService `json:"services"`
		Mounts   *[]store.Mount   `json:"mounts"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if wire.Services == nil || wire.Mounts == nil || etag == "" {
		return nil, errors.New("incomplete snapshot")
	}
	if wire.Vault == nil {
		return nil, fmt.Errorf("%w: the pull says so", ErrNoVault)
	}
	if wire.Vault.KeyID != s.KeyID {
		return nil, fmt.Errorf("%w: the account's key is %s, this device has %s", ErrKeyMismatch, wire.Vault.KeyID, s.KeyID)
	}
	snap := &Snapshot{Rev: etag, Mounts: *wire.Mounts, Vault: wire.Vault, Services: make([]store.Service, 0, len(*wire.Services))}
	for _, w := range *wire.Services {
		svc, legacy, err := s.open(w)
		if err != nil {
			return nil, err
		}
		if legacy {
			snap.Legacy = append(snap.Legacy, svc.ID)
		}
		snap.Services = append(snap.Services, svc)
	}
	return snap, nil
}

func (c *Client) PutService(ctx context.Context, s store.Service, baseVersion int64) (store.Service, error) {
	if c.Sealer != nil {
		return c.putSealed(ctx, s, baseVersion)
	}
	body := struct {
		Name        string            `json:"name"`
		Type        string            `json:"type"`
		Options     map[string]string `json:"options"`
		Secrets     map[string]string `json:"secrets"`
		BaseVersion int64             `json:"baseVersion"`
	}{s.Name, s.Type, nonNil(s.Options), nonNil(s.Secrets), baseVersion}
	var out store.Service
	err := c.call(ctx, http.MethodPut, "/services/"+url.PathEscape(s.ID), true, body, &out, http.StatusOK)
	return out, err
}

func (c *Client) putSealed(ctx context.Context, s store.Service, baseVersion int64) (store.Service, error) {
	body, err := c.Sealer.body(s, baseVersion)
	if err != nil {
		return store.Service{}, err
	}
	var out sealedService
	if err := c.call(ctx, http.MethodPut, "/services/"+url.PathEscape(s.ID), true, body, &out, http.StatusOK); err != nil {
		return store.Service{}, c.Sealer.openConflict(err)
	}
	saved, _, err := c.Sealer.open(out)
	return saved, err
}

func (c *Client) DeleteService(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/services/"+url.PathEscape(id), true, nil, nil, http.StatusNoContent)
}

func (c *Client) PutMount(ctx context.Context, m store.Mount, baseVersion int64) (store.Mount, error) {
	body := struct {
		Name        string    `json:"name"`
		ServiceID   string    `json:"serviceId"`
		RemotePath  string    `json:"remotePath"`
		VFS         store.VFS `json:"vfs"`
		BaseVersion int64     `json:"baseVersion"`
	}{m.Name, m.ServiceID, m.RemotePath, m.VFS, baseVersion}
	var out store.Mount
	err := c.call(ctx, http.MethodPut, "/mounts/"+url.PathEscape(m.ID), true, body, &out, http.StatusOK)
	return out, err
}

func (c *Client) DeleteMount(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/mounts/"+url.PathEscape(id), true, nil, nil, http.StatusNoContent)
}

// sealed, the token is the only field that refreshes, so field is always "token" there
func (c *Client) WriteToken(ctx context.Context, serviceID, field, value string, expiry time.Time) error {
	if c.Sealer != nil {
		sealed, err := vault.Seal(c.Sealer.AK, []byte(value), vault.AADServiceToken(serviceID))
		if err != nil {
			return err
		}
		body := map[string]string{"keyId": c.Sealer.KeyID, "sealedToken": sealed, "expiry": expiry.UTC().Format(time.RFC3339)}
		return c.call(ctx, http.MethodPost, "/services/"+url.PathEscape(serviceID)+"/token", true, body, nil, http.StatusOK)
	}
	body := map[string]string{"field": field, "value": value, "expiry": expiry.UTC().Format(time.RFC3339)}
	return c.call(ctx, http.MethodPost, "/services/"+url.PathEscape(serviceID)+"/token", true, body, nil, http.StatusOK)
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var out []Device
	err := c.call(ctx, http.MethodGet, "/devices", true, nil, &out, http.StatusOK)
	return out, err
}

func (c *Client) Revoke(ctx context.Context, id, mode string) error {
	return c.call(ctx, http.MethodDelete, "/devices/"+url.PathEscape(id)+"?mode="+url.QueryEscape(mode), true, nil, nil, http.StatusNoContent)
}

func (c *Client) Erased(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/devices/self/erased", true, nil, nil, http.StatusNoContent)
}

func (c *Client) Me(ctx context.Context) (Me, error) {
	var out Me
	err := c.call(ctx, http.MethodGet, "/me", true, nil, &out, http.StatusOK)
	return out, err
}

func (c *Client) RenameDevice(ctx context.Context, id, name string) error {
	return c.call(ctx, http.MethodPatch, "/devices/"+url.PathEscape(id), true, map[string]string{"name": name}, nil, http.StatusNoContent)
}

func (c *Client) ReportDrives(ctx context.Context, mountIDs []string) error {
	if mountIDs == nil {
		mountIDs = []string{}
	}
	return c.call(ctx, http.MethodPut, "/devices/self/drives", true, map[string][]string{"mountIds": mountIDs}, nil, http.StatusNoContent)
}

func (c *Client) PostEvents(ctx context.Context, events []api.Event) error {
	return c.call(ctx, http.MethodPost, "/events", true, map[string][]api.Event{"entries": events}, nil, http.StatusNoContent)
}

func (c *Client) Events(ctx context.Context, limit int) ([]api.Event, error) {
	var out struct{ Items []api.Event }
	err := c.call(ctx, http.MethodGet, "/events?limit="+strconv.Itoa(limit), true, nil, &out, http.StatusOK)
	return out.Items, err
}

func Offline(err error) bool {
	var ue *url.Error
	return errors.As(err, &ue)
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
