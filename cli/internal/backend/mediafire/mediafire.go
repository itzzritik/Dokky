package mediafire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/fserrors"
	"github.com/rclone/rclone/lib/encoder"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/rclone/rclone/lib/rest"
)

const (
	defaultAppID  = "42511"
	minSleep      = 100 * time.Millisecond
	maxSleep      = 2 * time.Second
	decayConstant = 2
)

var apiURL = "https://www.mediafire.com/api/1.5"

var retryErrorCodes = []int{429, 500, 502, 503, 504}

type Options struct {
	Email             string               `config:"email"`
	Password          string               `config:"password"`
	AppID             string               `config:"app_id"`
	UploadConcurrency int                  `config:"upload_concurrency"`
	Enc               encoder.MultiEncoder `config:"encoding"`
}

type Fs struct {
	name  string
	root  string
	opt   Options
	srv   *rest.Client
	pacer *fs.Pacer
	sess  *session
}

type apiResult interface{ Err() error }

func (f *Fs) call(ctx context.Context, method string, params url.Values, result apiResult) error {
	for attempt := 0; ; attempt++ {
		token, err := f.sess.Token(ctx)
		if err != nil {
			return err
		}
		p := maps.Clone(params)
		if p == nil {
			p = url.Values{}
		}
		p.Set("session_token", token)
		p.Set("response_format", "json")
		env := struct {
			Response any `json:"response"`
		}{result}
		err = postForm(ctx, f.srv, f.pacer, method, p, &env)
		if err == nil {
			err = result.Err()
		}
		var apiErr *api.Error
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Code == api.CodeSessionInvalid {
			f.sess.Invalidate(token)
			continue
		}
		return err
	}
}

func newPacer(ctx context.Context) *fs.Pacer {
	return fs.NewPacer(ctx, pacer.NewDefault(pacer.MinSleep(minSleep), pacer.MaxSleep(maxSleep), pacer.DecayConstant(decayConstant)))
}

func shouldRetry(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if fserrors.ContextError(ctx, &err) {
		return false, err
	}
	return fserrors.ShouldRetry(err) || fserrors.ShouldRetryHTTP(resp, retryErrorCodes), err
}

func errorHandler(resp *http.Response) error {
	body, err := rest.ReadBody(resp)
	if err != nil {
		return fmt.Errorf("mediafire: HTTP %s: %w", resp.Status, err)
	}
	var out struct {
		Response api.Base `json:"response"`
	}
	if json.Unmarshal(body, &out) == nil && out.Response.Result != "" {
		if err := out.Response.Err(); err != nil {
			return err
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200]
	}
	return fmt.Errorf("mediafire: HTTP %s: %s", resp.Status, text)
}

func postForm(ctx context.Context, srv *rest.Client, p *fs.Pacer, method string, params url.Values, out any) error {
	body := params.Encode()
	opts := rest.Opts{
		Method:      "POST",
		RootURL:     apiURL,
		Path:        "/" + method + ".php",
		ContentType: "application/x-www-form-urlencoded",
	}
	return p.Call(func() (bool, error) {
		opts.Body = strings.NewReader(body)
		resp, err := srv.CallJSON(ctx, &opts, nil, out)
		return shouldRetry(ctx, resp, err)
	})
}
