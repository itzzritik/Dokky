package mediafire

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/rest"
)

var (
	now             = time.Now
	sessionLifetime = 10 * time.Minute
	renewMargin     = 2 * time.Minute
	idleStop        = time.Hour

	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
)

type session struct {
	mu                     sync.Mutex
	email, password, appID string
	srv                    *rest.Client
	pacer                  *fs.Pacer
	token                  string
	expires                time.Time
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
	if err := postForm(ctx, s.srv, s.pacer, "user/get_session_token", params, &out); err != nil {
		return err
	}
	if err := out.Response.Err(); err != nil {
		return err
	}
	s.token = out.Response.SessionToken
	s.expires = now().Add(sessionLifetime)
	return nil
}
