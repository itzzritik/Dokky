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
	"time"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/itzzritik/dokky/cli/internal/store"
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
}

type Snapshot struct {
	Rev      string
	Services []store.Service
	Mounts   []store.Mount
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.Token)
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
	}
	_ = json.Unmarshal(data, &env)
	code, msg := env.Error.Code, env.Error.Message
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized && code == "device_revoked":
		return fmt.Errorf("%w: %s", ErrRevoked, msg)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case resp.StatusCode == http.StatusConflict && code == "conflict":
		return &ConflictError{Current: env.Current}
	case resp.StatusCode == http.StatusConflict && code == "name_taken":
		return fmt.Errorf("%w: %s", ErrNameTaken, msg)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	}
	return &StatusError{Status: resp.StatusCode, Code: code, Message: msg}
}

func (c *Client) call(ctx context.Context, method, path string, auth bool, body, out any, want int) error {
	resp, data, err := c.do(ctx, method, path, auth, nil, body)
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
	var wire struct {
		Services *[]store.Service `json:"services"`
		Mounts   *[]store.Mount   `json:"mounts"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, false, fmt.Errorf("decode snapshot: %w", err)
	}
	etag := resp.Header.Get("ETag")
	if wire.Services == nil || wire.Mounts == nil || etag == "" {
		return nil, false, errors.New("incomplete snapshot")
	}
	return &Snapshot{Rev: etag, Services: *wire.Services, Mounts: *wire.Mounts}, false, nil
}

func (c *Client) PutService(ctx context.Context, s store.Service, baseVersion int64) (store.Service, error) {
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

func (c *Client) WriteToken(ctx context.Context, serviceID, field, value string, expiry time.Time) error {
	body := map[string]string{"field": field, "value": value, "expiry": expiry.UTC().Format(time.RFC3339)}
	return c.call(ctx, http.MethodPost, "/services/"+url.PathEscape(serviceID)+"/token", true, body, nil, http.StatusOK)
}

func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	var out []Device
	err := c.call(ctx, http.MethodGet, "/devices", true, nil, &out, http.StatusOK)
	return out, err
}

func (c *Client) Revoke(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/devices/"+url.PathEscape(id), true, nil, nil, http.StatusNoContent)
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
