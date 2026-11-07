package mediafire

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/rest"
)

var (
	// wall clock: the monotonic one stops while the computer sleeps
	now             = func() time.Time { return time.Now().Round(0) }
	sleep           = time.Sleep
	sessionLifetime = 10 * time.Minute
	renewMargin     = 2 * time.Minute
	idleStop        = time.Hour
	loginBackoff    = 5 * time.Minute

	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
)

type session struct {
	mu                     sync.Mutex
	email, password, appID string
	srv                    *rest.Client
	pacer                  *fs.Pacer
	token                  string
	expires, lastUse       time.Time
	alive                  bool
	loginErr               error
	loginErrAt             time.Time
}

func signature(email, password, appID string) string {
	sum := sha1.Sum([]byte(email + password + appID))
	return hex.EncodeToString(sum[:])
}

func getSession(email, password, appID string, srv *rest.Client, p *fs.Pacer) *session {
	key := appID + "\x00" + email
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if s := sessions[key]; s != nil && s.password == password {
		return s
	}
	s := &session{email: email, password: password, appID: appID, srv: srv, pacer: p}
	sessions[key] = s
	return s
}

func (s *session) login(ctx context.Context) error {
	if s.loginErr != nil && now().Sub(s.loginErrAt) < loginBackoff {
		return s.loginErr
	}
	params := url.Values{
		"email":           {s.email},
		"password":        {s.password},
		"application_id":  {s.appID},
		"token_version":   {"1"},
		"response_format": {"json"},
		"signature":       {signature(s.email, s.password, s.appID)},
	}
	var out struct {
		Response api.SessionToken `json:"response"`
	}
	err := postForm(ctx, s.srv, s.pacer, "user/get_session_token", params, &out)
	if err == nil {
		err = out.Response.Err()
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		s.loginErr, s.loginErrAt = err, now()
		return err
	}
	if err != nil {
		return err
	}
	s.loginErr = nil
	s.token = out.Response.SessionToken
	s.expires = now().Add(sessionLifetime)
	return nil
}

func (s *session) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUse = now()
	if s.token == "" || !now().Before(s.expires.Add(-renewMargin)) {
		if err := s.refresh(ctx); err != nil {
			return "", err
		}
	}
	if !s.alive {
		s.alive = true
		go s.keepAlive()
	}
	return s.token, nil
}

func (s *session) Invalidate(token string) {
	s.mu.Lock()
	if s.token == token {
		s.token = ""
	}
	s.mu.Unlock()
}

func (s *session) refresh(ctx context.Context) error {
	if s.token != "" && now().Before(s.expires) && s.renew(ctx) == nil {
		return nil
	}
	return s.login(ctx)
}

func (s *session) renew(ctx context.Context) error {
	params := url.Values{"session_token": {s.token}, "response_format": {"json"}}
	var out struct {
		Response api.SessionToken `json:"response"`
	}
	if err := postForm(ctx, s.srv, s.pacer, "user/renew_session_token", params, &out); err != nil {
		return err
	}
	if err := out.Response.Err(); err != nil {
		return err
	}
	if out.Response.SessionToken != "" {
		s.token = out.Response.SessionToken
	}
	s.expires = now().Add(sessionLifetime)
	return nil
}

func (s *session) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}

func (s *session) keepAlive() {
	for {
		s.mu.Lock()
		wait := s.expires.Add(-renewMargin).Sub(now())
		s.mu.Unlock()
		// a failing refresh leaves expires behind, so never spin
		sleep(max(wait, time.Minute))
		if !s.keepAliveStep(context.Background()) {
			return
		}
	}
}

func (s *session) keepAliveStep(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now().Sub(s.lastUse) > idleStop {
		s.alive = false
		return false
	}
	if s.token != "" && !now().Before(s.expires.Add(-renewMargin)) {
		if err := s.refresh(ctx); err != nil {
			fs.Debugf(nil, "mediafire: keep session alive: %v", err)
		}
	}
	return true
}
