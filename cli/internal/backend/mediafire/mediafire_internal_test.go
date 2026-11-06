package mediafire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs/fshttp"
	"github.com/rclone/rclone/lib/rest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type route func(form url.Values, r *http.Request) any

type fakeAPI struct {
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]route
	raw    map[string]http.HandlerFunc
	calls  map[string][]url.Values
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{routes: map[string]route{}, raw: map[string]http.HandlerFunc{}, calls: map[string][]url.Values{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	old := apiURL
	apiURL = f.srv.URL
	t.Cleanup(func() {
		apiURL = old
		f.srv.Close()
		sessionsMu.Lock()
		clear(sessions)
		sessionsMu.Unlock()
	})
	return f
}

func (f *fakeAPI) handle(name string, fn route) {
	f.mu.Lock()
	f.routes[name] = fn
	f.mu.Unlock()
}

func (f *fakeAPI) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls[name])
}

func (f *fakeAPI) last(name string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls[name]
	if len(c) == 0 {
		return nil
	}
	return c[len(c)-1]
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	raw := f.raw[r.URL.Path]
	f.mu.Unlock()
	if raw != nil {
		raw(w, r)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".php")
	_ = r.ParseForm()
	f.mu.Lock()
	f.calls[name] = append(f.calls[name], r.Form)
	fn := f.routes[name]
	f.mu.Unlock()
	if fn == nil {
		http.Error(w, "no route "+name, http.StatusNotFound)
		return
	}
	out := fn(r.Form, r)
	status := http.StatusOK
	if m, ok := out.(map[string]any); ok && m["result"] == "Error" {
		status = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"response": out})
}

func success(kv ...any) map[string]any {
	m := map[string]any{"result": "Success"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func apiError(code int, msg string) map[string]any {
	return map[string]any{"result": "Error", "error": code, "message": msg}
}

func testClient(ctx context.Context) *rest.Client {
	return rest.NewClient(fshttp.NewClient(ctx)).SetErrorHandler(errorHandler)
}

func TestSignature(t *testing.T) {
	assert.Equal(t, "fd41f8ed4283ad63baf5348624ad7ebc613e2981", signature("user@example.com", "password", "42511"))
}

func TestLoginSendsSignedForm(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("user/get_session_token", func(url.Values, *http.Request) any {
		return success("session_token", "tok1")
	})
	s := getSession("user@example.com", "password", defaultAppID, testClient(ctx), newPacer(ctx))
	require.NoError(t, s.login(ctx))
	assert.Equal(t, "tok1", s.token)
	form := fake.last("user/get_session_token")
	assert.Equal(t, "user@example.com", form.Get("email"))
	assert.Equal(t, "password", form.Get("password"))
	assert.Equal(t, "42511", form.Get("application_id"))
	assert.Equal(t, "1", form.Get("token_version"))
	assert.Equal(t, "json", form.Get("response_format"))
	assert.Equal(t, signature("user@example.com", "password", "42511"), form.Get("signature"))
}

func TestLoginBadCredentials(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("user/get_session_token", func(url.Values, *http.Request) any {
		return apiError(107, "The Credentials you entered are invalid")
	})
	s := getSession("user@example.com", "wrong", defaultAppID, testClient(ctx), newPacer(ctx))
	err := s.login(ctx)
	var apiErr *api.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 107, apiErr.Code)
	assert.Contains(t, err.Error(), "The Credentials you entered are invalid")
	assert.Equal(t, 1, fake.count("user/get_session_token"))
}

func TestSessionSharedByEmail(t *testing.T) {
	ctx := context.Background()
	newFakeAPI(t)
	a := getSession("user@example.com", "password", defaultAppID, testClient(ctx), newPacer(ctx))
	b := getSession("user@example.com", "password", defaultAppID, testClient(ctx), newPacer(ctx))
	assert.Same(t, a, b)
}

func TestSessionReplacedWhenPasswordChanges(t *testing.T) {
	ctx := context.Background()
	newFakeAPI(t)
	a := getSession("user@example.com", "old", defaultAppID, testClient(ctx), newPacer(ctx))
	b := getSession("user@example.com", "new", defaultAppID, testClient(ctx), newPacer(ctx))
	assert.NotSame(t, a, b)
	assert.Equal(t, "new", b.password)
}
