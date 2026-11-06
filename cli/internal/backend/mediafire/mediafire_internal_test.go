package mediafire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/backend/mediafire/api"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/fshttp"
	"github.com/rclone/rclone/fs/hash"
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

type fakeClock struct {
	mu  sync.Mutex
	cur time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.cur = c.cur.Add(d)
	c.mu.Unlock()
}

func stubTime(t *testing.T) *fakeClock {
	c := &fakeClock{cur: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	oldNow, oldSleep := now, sleep
	now = c.Now
	sleep = func(time.Duration) { select {} }
	t.Cleanup(func() { now, sleep = oldNow, oldSleep })
	return c
}

func newTestFs(t *testing.T, fake *fakeAPI) *Fs {
	ctx := context.Background()
	oldSleep := sleep
	sleep = func(time.Duration) { select {} }
	t.Cleanup(func() { sleep = oldSleep })
	fake.mu.Lock()
	_, hasLogin := fake.routes["user/get_session_token"]
	fake.mu.Unlock()
	if !hasLogin {
		tokens(fake, "user/get_session_token", "session_token")
	}
	f, err := NewFs(ctx, "TestFake", "", configmap.Simple{"email": "user@example.com", "password": obscure.MustObscure("password")})
	require.NoError(t, err)
	return f.(*Fs)
}

func folderListing(folders []map[string]any, files []map[string]any) route {
	return func(form url.Values, _ *http.Request) any {
		if form.Get("content_type") == "folders" {
			return success("folder_content", map[string]any{"folders": folders, "more_chunks": "no"})
		}
		return success("folder_content", map[string]any{"files": files, "more_chunks": "no"})
	}
}

func tokens(fake *fakeAPI, name, field string) {
	n := 0
	var mu sync.Mutex
	fake.handle(name, func(url.Values, *http.Request) any {
		mu.Lock()
		defer mu.Unlock()
		n++
		return success(field, fmt.Sprintf("tok%d", n))
	})
}

func TestTokenRenewsWhenExpired(t *testing.T) {
	ctx := context.Background()
	clock := stubTime(t)
	fake := newFakeAPI(t)
	tokens(fake, "user/get_session_token", "session_token")
	fake.handle("user/renew_session_token", func(url.Values, *http.Request) any {
		return success("session_token", "renewed")
	})
	f := newTestFs(t, fake)
	tok, err := f.sess.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tok1", tok)
	clock.Add(9 * time.Minute)
	tok, err = f.sess.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "renewed", tok)
	assert.Equal(t, 1, fake.count("user/renew_session_token"))
	assert.Equal(t, "tok1", fake.last("user/renew_session_token").Get("session_token"))
	assert.Equal(t, 1, fake.count("user/get_session_token"))
}

func TestTokenAfterSleepSignsIn(t *testing.T) {
	ctx := context.Background()
	clock := stubTime(t)
	fake := newFakeAPI(t)
	tokens(fake, "user/get_session_token", "session_token")
	fake.handle("user/renew_session_token", func(url.Values, *http.Request) any {
		return apiError(105, "The supplied Session Token is expired or invalid")
	})
	f := newTestFs(t, fake)
	_, err := f.sess.Token(ctx)
	require.NoError(t, err)
	clock.Add(2 * time.Hour)
	tok, err := f.sess.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tok2", tok)
	assert.Equal(t, 0, fake.count("user/renew_session_token"))
	assert.Equal(t, 2, fake.count("user/get_session_token"))
}

func TestCallRetriesOnceOnSessionError(t *testing.T) {
	ctx := context.Background()
	stubTime(t)
	fake := newFakeAPI(t)
	tokens(fake, "user/get_session_token", "session_token")
	n := 0
	fake.handle("folder/get_content", func(url.Values, *http.Request) any {
		n++
		if n == 1 {
			return apiError(105, "The supplied Session Token is expired or invalid")
		}
		return success()
	})
	f := newTestFs(t, fake)
	var out api.FolderContent
	require.NoError(t, f.call(ctx, "folder/get_content", url.Values{"content_type": {"files"}}, &out))
	assert.Equal(t, 2, fake.count("user/get_session_token"))
	assert.Equal(t, 2, fake.count("folder/get_content"))
	form := fake.last("folder/get_content")
	assert.Equal(t, "tok2", form.Get("session_token"))
	assert.Equal(t, "json", form.Get("response_format"))
	assert.Equal(t, "files", form.Get("content_type"))
}

func TestCallGivesUpAfterSecondSessionError(t *testing.T) {
	ctx := context.Background()
	stubTime(t)
	fake := newFakeAPI(t)
	tokens(fake, "user/get_session_token", "session_token")
	fake.handle("folder/get_content", func(url.Values, *http.Request) any {
		return apiError(105, "The supplied Session Token is expired or invalid")
	})
	f := newTestFs(t, fake)
	var out api.FolderContent
	err := f.call(ctx, "folder/get_content", nil, &out)
	var apiErr *api.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, api.CodeSessionInvalid, apiErr.Code)
	assert.Equal(t, 2, fake.count("folder/get_content"))
}

func TestKeepAliveRenewsWhileUsedAndStopsWhenIdle(t *testing.T) {
	ctx := context.Background()
	clock := stubTime(t)
	fake := newFakeAPI(t)
	tokens(fake, "user/get_session_token", "session_token")
	tokens(fake, "user/renew_session_token", "session_token")
	f := newTestFs(t, fake)
	_, err := f.sess.Token(ctx)
	require.NoError(t, err)
	assert.True(t, f.sess.running())

	clock.Add(9 * time.Minute)
	assert.True(t, f.sess.keepAliveStep(ctx))
	assert.Equal(t, 1, fake.count("user/renew_session_token"))

	clock.Add(idleStop)
	assert.False(t, f.sess.keepAliveStep(ctx))
	assert.False(t, f.sess.running())
	assert.Equal(t, 1, fake.count("user/renew_session_token"))

	_, err = f.sess.Token(ctx)
	require.NoError(t, err)
	assert.True(t, f.sess.running())
}

func TestListPagesFoldersAndFiles(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("folder/get_content", func(form url.Values, _ *http.Request) any {
		if form.Get("content_type") == "folders" {
			if form.Get("chunk") == "1" {
				return success("folder_content", map[string]any{"folders": []map[string]any{{"folderkey": "fA", "name": "A"}}, "more_chunks": "yes"})
			}
			return success("folder_content", map[string]any{"folders": []map[string]any{{"folderkey": "fB", "name": "B"}}, "more_chunks": "no"})
		}
		return success("folder_content", map[string]any{"files": []map[string]any{{
			"quickkey": "q1", "filename": "x.mp4", "size": "872632354", "hash": "ab12", "created_utc": "2026-11-07T10:00:00Z",
		}}, "more_chunks": "no"})
	})
	f := newTestFs(t, fake)
	entries, err := f.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "A", entries[0].Remote())
	assert.Equal(t, "B", entries[1].Remote())
	o, ok := entries[2].(fs.Object)
	require.True(t, ok)
	assert.Equal(t, "x.mp4", o.Remote())
	assert.Equal(t, int64(872632354), o.Size())
	sum, err := o.Hash(ctx, hash.SHA256)
	require.NoError(t, err)
	assert.Equal(t, "ab12", sum)
	assert.Equal(t, time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC), o.ModTime(ctx).UTC())
	fake.mu.Lock()
	first := fake.calls["folder/get_content"][0]
	fake.mu.Unlock()
	assert.Equal(t, "myfiles", first.Get("folder_key"))
	assert.Equal(t, "yes", first.Get("details"))
	assert.Equal(t, "1000", first.Get("chunk_size"))
}

func TestListKeepsDuplicateNames(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("folder/get_content", folderListing(nil, []map[string]any{
		{"quickkey": "q1", "filename": "a.txt", "size": "1"},
		{"quickkey": "q2", "filename": "a.txt", "size": "2"},
	}))
	f := newTestFs(t, fake)
	entries, err := f.List(ctx, "")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestNewObjectNotFound(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("folder/get_content", folderListing(nil, nil))
	f := newTestFs(t, fake)
	_, err := f.NewObject(ctx, "nope.txt")
	assert.ErrorIs(t, err, fs.ErrorObjectNotFound)
}

func TestAbout(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("user/get_info", func(url.Values, *http.Request) any {
		return success("user_info", map[string]any{"used_storage_size": "1000", "storage_limit": "10737418240"})
	})
	f := newTestFs(t, fake)
	u, err := f.About(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), *u.Used)
	assert.Equal(t, int64(10737418240), *u.Total)
	assert.Equal(t, int64(10737417240), *u.Free)
}

func linkTo(fake *fakeAPI, p string) {
	fake.handle("file/get_links", func(url.Values, *http.Request) any {
		return success("links", []map[string]any{{"quickkey": "q1", "direct_download": fake.srv.URL + p}})
	})
}

func testObject(f *Fs, size int64) *Object {
	return &Object{fs: f, remote: "x.mp4", id: "q1", size: size}
}

func TestOpenPassesRange(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	linkTo(fake, "/dl/x.mp4")
	fake.raw["/dl/x.mp4"] = func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bytes=436316177-437364752", r.Header.Get("Range"))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(make([]byte, 1048576))
	}
	f := newTestFs(t, fake)
	rc, err := testObject(f, 872632354).Open(ctx, &fs.RangeOption{Start: 436316177, End: 437364752})
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Len(t, b, 1048576)
	assert.Equal(t, "q1", fake.last("file/get_links").Get("quick_key"))
	assert.Equal(t, "direct_download", fake.last("file/get_links").Get("link_type"))
}

func TestOpenRetriesConnectionReset(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	linkTo(fake, "/dl/x.mp4")
	var hits atomic.Int32
	fake.raw["/dl/x.mp4"] = func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte("hello"))
	}
	f := newTestFs(t, fake)
	rc, err := testObject(f, 5).Open(ctx)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
	assert.Equal(t, int32(2), hits.Load())
}

func TestOpenWithoutLinkFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("file/get_links", func(url.Values, *http.Request) any {
		return success("links", []map[string]any{{"quickkey": "q1", "direct_download": ""}})
	})
	f := newTestFs(t, fake)
	rc, err := testObject(f, 5).Open(ctx)
	assert.ErrorContains(t, err, "no download link")
	assert.Nil(t, rc)
	assert.Equal(t, 1, fake.count("file/get_links"))
}
