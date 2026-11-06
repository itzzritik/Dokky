package mediafire

import (
	"context"
	"encoding/json"
	"fmt"
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
