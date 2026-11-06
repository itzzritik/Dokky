package mediafire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
	"github.com/rclone/rclone/fs/object"
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

type uploadFake struct {
	mu      sync.Mutex
	units   map[string][]byte
	headers map[string]http.Header
	forms   []url.Values
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func fakeUpload(t *testing.T, fake *fakeAPI, check map[string]any, polls ...map[string]any) *uploadFake {
	old := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	u := &uploadFake{units: map[string][]byte{}, headers: map[string]http.Header{}}
	fake.handle("upload/check", func(url.Values, *http.Request) any {
		return success(append([]any{}, flatten(check)...)...)
	})
	fake.handle("upload/resumable", func(form url.Values, r *http.Request) any {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		id := r.Header.Get("x-unit-id")
		u.mu.Lock()
		u.units[id] = b
		u.headers[id] = r.Header.Clone()
		u.forms = append(u.forms, form)
		u.mu.Unlock()
		return success("doupload", map[string]any{"result": "0", "key": "k1"})
	})
	n := 0
	fake.handle("upload/poll_upload", func(url.Values, *http.Request) any {
		p := polls[min(n, len(polls)-1)]
		n++
		return success("doupload", p)
	})
	return u
}

func flatten(m map[string]any) []any {
	var kv []any
	for k, v := range m {
		kv = append(kv, k, v)
	}
	return kv
}

func resumable(units, unitSize int, words ...string) map[string]any {
	return map[string]any{
		"hash_exists": "no",
		"resumable_upload": map[string]any{
			"number_of_units": strconv.Itoa(units),
			"unit_size":       strconv.Itoa(unitSize),
			"upload_key":      "k1",
			"bitmap":          map[string]any{"count": strconv.Itoa(len(words)), "words": words},
		},
	}
}

var finished = map[string]any{"result": "0", "status": "99", "quickkey": "q1", "size": "10"}

func srcInfo(content string, withHash bool) fs.ObjectInfo {
	var hashes map[hash.Type]string
	if withHash {
		hashes = map[hash.Type]string{hash.SHA256: sha256Hex([]byte(content))}
	}
	return object.NewStaticObjectInfo("v.bin", time.Now(), int64(len(content)), true, hashes, nil)
}

func TestUnitDone(t *testing.T) {
	words := []string{"5", "1"}
	assert.True(t, unitDone(words, 0))
	assert.False(t, unitDone(words, 1))
	assert.True(t, unitDone(words, 2))
	assert.True(t, unitDone(words, 16))
	assert.False(t, unitDone(words, 40))
}

func TestPutUploadsUnitsAndPolls(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	u := fakeUpload(t, fake, resumable(3, 4), map[string]any{"result": "0", "status": "17"}, finished)
	f := newTestFs(t, fake)
	o, err := f.Put(ctx, strings.NewReader(content), srcInfo(content, true))
	require.NoError(t, err)
	assert.Equal(t, "q1", o.(*Object).id)
	assert.Equal(t, int64(10), o.Size())
	require.Len(t, u.units, 3)
	for id, want := range map[string]string{"0": "0123", "1": "4567", "2": "89"} {
		assert.Equal(t, want, string(u.units[id]))
		h := u.headers[id]
		assert.Equal(t, strconv.Itoa(len(want)), h.Get("x-unit-size"))
		assert.Equal(t, sha256Hex([]byte(want)), h.Get("x-unit-hash"))
		assert.Equal(t, sha256Hex([]byte(content)), h.Get("x-filehash"))
		assert.Equal(t, "10", h.Get("x-filesize"))
		assert.Equal(t, "v.bin", h.Get("x-filename"))
	}
	assert.Equal(t, "replace", u.forms[0].Get("action_on_duplicate"))
	assert.Equal(t, "myfiles", u.forms[0].Get("folder_key"))
	assert.Equal(t, 2, fake.count("upload/poll_upload"))
	assert.Equal(t, "k1", fake.last("upload/poll_upload").Get("key"))
	check := fake.last("upload/check")
	assert.Equal(t, "v.bin", check.Get("filename"))
	assert.Equal(t, "10", check.Get("size"))
	assert.Equal(t, sha256Hex([]byte(content)), check.Get("hash"))
	assert.Equal(t, "yes", check.Get("resumable"))
}

func TestPutSkipsUnitsAlreadyDone(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	u := fakeUpload(t, fake, resumable(3, 4, "1"), finished)
	f := newTestFs(t, fake)
	_, err := f.Put(ctx, strings.NewReader(content), srcInfo(content, true))
	require.NoError(t, err)
	assert.Len(t, u.units, 2)
	assert.Equal(t, "4567", string(u.units["1"]))
	assert.Equal(t, "89", string(u.units["2"]))
}

func TestPutInstantWhenHashExists(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	u := fakeUpload(t, fake, map[string]any{"hash_exists": "yes"}, finished)
	fake.handle("upload/instant", func(url.Values, *http.Request) any {
		return success("quickkey", "qi", "filename", "v.bin")
	})
	f := newTestFs(t, fake)
	o, err := f.Put(ctx, strings.NewReader(content), srcInfo(content, true))
	require.NoError(t, err)
	assert.Equal(t, "qi", o.(*Object).id)
	assert.Empty(t, u.units)
	form := fake.last("upload/instant")
	assert.Equal(t, sha256Hex([]byte(content)), form.Get("hash"))
	assert.Equal(t, "10", form.Get("size"))
	assert.Equal(t, "v.bin", form.Get("filename"))
	assert.Equal(t, "replace", form.Get("action_on_duplicate"))
}

func TestPutHashesUnknownSource(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	u := fakeUpload(t, fake, resumable(1, 16), finished)
	f := newTestFs(t, fake)
	_, err := f.Put(ctx, strings.NewReader(content), srcInfo(content, false))
	require.NoError(t, err)
	assert.Equal(t, sha256Hex([]byte(content)), fake.last("upload/check").Get("hash"))
	assert.Equal(t, content, string(u.units["0"]))
}

func TestUpdateSendsQuickKey(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	u := fakeUpload(t, fake, resumable(1, 16), finished)
	f := newTestFs(t, fake)
	o := &Object{fs: f, remote: "v.bin", id: "q0", size: 3}
	require.NoError(t, o.Update(ctx, strings.NewReader(content), srcInfo(content, true)))
	require.Len(t, u.forms, 1)
	assert.Equal(t, "q0", u.forms[0].Get("quick_key"))
	assert.Empty(t, u.forms[0].Get("action_on_duplicate"))
	assert.Equal(t, int64(10), o.Size())
}

func TestUploadPollFileErrorFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	content := "0123456789"
	fakeUpload(t, fake, resumable(1, 16), map[string]any{"result": "0", "status": "99", "fileerror": "5"})
	f := newTestFs(t, fake)
	o, err := f.Put(ctx, strings.NewReader(content), srcInfo(content, true))
	assert.Error(t, err)
	assert.Nil(t, o)
}

func TestMkdirCreatesFolder(t *testing.T) {
	ctx := context.Background()
	fake := newFakeAPI(t)
	fake.handle("folder/get_content", folderListing(nil, nil))
	fake.handle("folder/create", func(url.Values, *http.Request) any {
		return success("folder_key", "fNew")
	})
	f := newTestFs(t, fake)
	require.NoError(t, f.Mkdir(ctx, "New"))
	form := fake.last("folder/create")
	assert.Equal(t, "New", form.Get("foldername"))
	assert.Equal(t, "myfiles", form.Get("parent_key"))
}

type node struct{ folders, files []map[string]any }

func tree(nodes map[string]node) route {
	return func(form url.Values, _ *http.Request) any {
		n := nodes[form.Get("folder_key")]
		if form.Get("content_type") == "folders" {
			return success("folder_content", map[string]any{"folders": n.folders, "more_chunks": "no"})
		}
		return success("folder_content", map[string]any{"files": n.files, "more_chunks": "no"})
	}
}

func okRoute(url.Values, *http.Request) any { return success() }

func folder(key, name string) map[string]any { return map[string]any{"folderkey": key, "name": name} }

func file(key, name string) map[string]any {
	return map[string]any{"quickkey": key, "filename": name, "size": "1"}
}

func moveFs(t *testing.T) (*Fs, *fakeAPI) {
	fake := newFakeAPI(t)
	fake.handle("folder/get_content", tree(map[string]node{
		"myfiles": {folders: []map[string]any{folder("fA", "A"), folder("fB", "B")}, files: []map[string]any{file("q1", "a.txt")}},
		"fA":      {files: []map[string]any{file("q9", "inside.txt")}},
	}))
	for _, r := range []string{"file/purge", "file/delete", "folder/purge", "file/move", "file/update", "folder/move", "folder/update"} {
		fake.handle(r, okRoute)
	}
	return newTestFs(t, fake), fake
}

func TestRemovePurgesFile(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	require.NoError(t, testObject(f, 1).Remove(ctx))
	assert.Equal(t, "q1", fake.last("file/purge").Get("quick_key"))
	assert.Equal(t, 0, fake.count("file/delete"))
}

func TestRmdirRefusesNonEmpty(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	assert.ErrorIs(t, f.Rmdir(ctx, "A"), fs.ErrorDirectoryNotEmpty)
	assert.Equal(t, 0, fake.count("folder/purge"))
}

func TestRmdirPurgesEmptyFolder(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	require.NoError(t, f.Rmdir(ctx, "B"))
	assert.Equal(t, "fB", fake.last("folder/purge").Get("folder_key"))
}

func TestPurgeRemovesFolderWithContents(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	require.NoError(t, f.Purge(ctx, "A"))
	assert.Equal(t, "fA", fake.last("folder/purge").Get("folder_key"))
}

func TestMoveRenameOnly(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	src := &Object{fs: f, remote: "a.txt", id: "q1", size: 1}
	dst, err := f.Move(ctx, src, "b.txt")
	require.NoError(t, err)
	assert.Equal(t, "b.txt", dst.Remote())
	assert.Equal(t, "q1", fake.last("file/update").Get("quick_key"))
	assert.Equal(t, "b.txt", fake.last("file/update").Get("filename"))
	assert.Equal(t, 0, fake.count("file/move"))
}

func TestMoveToOtherFolder(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	src := &Object{fs: f, remote: "a.txt", id: "q1", size: 1}
	dst, err := f.Move(ctx, src, "A/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "A/a.txt", dst.Remote())
	assert.Equal(t, "q1", fake.last("file/move").Get("quick_key"))
	assert.Equal(t, "fA", fake.last("file/move").Get("folder_key"))
	assert.Equal(t, 0, fake.count("file/update"))
}

func TestDirMoveMovesAndRenames(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	require.NoError(t, f.DirMove(ctx, f, "A", "B/C"))
	assert.Equal(t, "fA", fake.last("folder/move").Get("folder_key_src"))
	assert.Equal(t, "fB", fake.last("folder/move").Get("folder_key_dst"))
	assert.Equal(t, "fA", fake.last("folder/update").Get("folder_key"))
	assert.Equal(t, "C", fake.last("folder/update").Get("foldername"))
}

func TestCopyRenamesCopy(t *testing.T) {
	ctx := context.Background()
	f, fake := moveFs(t)
	fake.handle("file/copy", func(url.Values, *http.Request) any {
		return success("new_quickkeys", []string{"q2"})
	})
	src := &Object{fs: f, remote: "a.txt", id: "q1", size: 1}
	dst, err := f.Copy(ctx, src, "A/b.txt")
	require.NoError(t, err)
	assert.Equal(t, "q2", dst.(*Object).id)
	assert.Equal(t, "q1", fake.last("file/copy").Get("quick_key"))
	assert.Equal(t, "fA", fake.last("file/copy").Get("folder_key"))
	assert.Equal(t, "q2", fake.last("file/update").Get("quick_key"))
	assert.Equal(t, "b.txt", fake.last("file/update").Get("filename"))
}
