package daemon

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/rclone/rclone/fs"
	"github.com/stretchr/testify/require"
)

const (
	testToken    = "dky_sEcReTdEvIcEtOkEn0123456789abcdefghijklmnopq"
	testUserCode = "BCDF-GHJK"
)

type fakeAPI struct {
	srv *httptest.Server

	mu       sync.Mutex
	polls    []string
	services map[string]store.Service
	mounts   map[string]store.Mount
	puts     []string
	revoked  bool
	fail     map[string]string
	hook     func()
	rev      int
	reqs     []string
	down     bool
	devices  []dsync.Device
	revokes  []string
	expiry   map[string]time.Time
}

func newFakeAPI(t *testing.T, polls ...string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{polls: polls, services: map[string]store.Service{}, mounts: map[string]store.Mount{}, fail: map[string]string{}, expiry: map[string]time.Time{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/cli/login/start", func(w http.ResponseWriter, r *http.Request) {
		var info dsync.DeviceInfo
		if json.NewDecoder(r.Body).Decode(&info) != nil || info.DeviceName == "" || info.OS == "" {
			writeJSON(w, http.StatusBadRequest, apiError("bad_request"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"deviceCode": "device-code", "userCode": testUserCode,
			"verifyUrl": f.srv.URL + "/cli/approve?code=" + testUserCode, "interval": 2, "expiresIn": 600,
		})
	})
	mux.HandleFunc("POST /api/v1/cli/login/poll", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		status := f.polls[0]
		if len(f.polls) > 1 {
			f.polls = f.polls[1:]
		}
		if status != "approved" {
			writeJSON(w, http.StatusOK, map[string]string{"status": status})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status, "token": testToken, "deviceId": "dev1"})
	})
	device := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.down {
				if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					conn.Close()
				}
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				writeJSON(w, http.StatusUnauthorized, apiError("unauthorized"))
				return
			}
			f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
			if f.hook != nil {
				f.hook()
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/sync", device(func(w http.ResponseWriter, r *http.Request) {
		etag := fmt.Sprintf(`"%d"`, f.rev)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			f.reqs[len(f.reqs)-1] += " 304"
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rev": f.rev,
			"services": sortedBy(f.services, func(s store.Service) string { return s.Name }),
			"mounts":   sortedBy(f.mounts, func(m store.Mount) string { return m.Name })})
	}))
	mux.HandleFunc("PUT /api/v1/services/{id}", device(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			store.Service
			BaseVersion int64 `json:"baseVersion"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			writeJSON(w, http.StatusBadRequest, apiError("bad_request"))
			return
		}
		s := b.Service
		s.ID = r.PathValue("id")
		f.puts = append(f.puts, fmt.Sprintf("service %s %d", s.Name, b.BaseVersion))
		if msg, ok := f.fail[s.Name]; ok {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "internal", "message": msg}})
			return
		}
		cur, exists := f.services[s.ID]
		taken := slices.ContainsFunc(slices.Collect(maps.Values(f.services)), func(x store.Service) bool { return x.Name == s.Name && x.ID != s.ID })
		if save(w, exists, cur.Version, b.BaseVersion, taken, cur) {
			s.Version, s.UpdatedAt = b.BaseVersion+1, time.Now().UTC()
			f.services[s.ID] = s
			f.rev++
			writeJSON(w, http.StatusOK, s)
		}
	}))
	mux.HandleFunc("PUT /api/v1/mounts/{id}", device(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			store.Mount
			BaseVersion int64 `json:"baseVersion"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			writeJSON(w, http.StatusBadRequest, apiError("bad_request"))
			return
		}
		m := b.Mount
		m.ID = r.PathValue("id")
		f.puts = append(f.puts, fmt.Sprintf("mount %s %d", m.Name, b.BaseVersion))
		if msg, ok := f.fail[m.Name]; ok {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "internal", "message": msg}})
			return
		}
		if _, ok := f.services[m.ServiceID]; !ok {
			writeJSON(w, http.StatusBadRequest, apiError("bad_request"))
			return
		}
		cur, exists := f.mounts[m.ID]
		taken := slices.ContainsFunc(slices.Collect(maps.Values(f.mounts)), func(x store.Mount) bool { return x.Name == m.Name && x.ID != m.ID })
		if save(w, exists, cur.Version, b.BaseVersion, taken, cur) {
			m.Version, m.UpdatedAt = b.BaseVersion+1, time.Now().UTC()
			f.mounts[m.ID] = m
			f.rev++
			writeJSON(w, http.StatusOK, m)
		}
	}))
	mux.HandleFunc("DELETE /api/v1/devices/self", device(func(w http.ResponseWriter, _ *http.Request) {
		f.revoked = true
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("DELETE /api/v1/devices/{id}", device(func(w http.ResponseWriter, r *http.Request) {
		f.revokes = append(f.revokes, r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("GET /api/v1/devices", device(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, append([]dsync.Device{}, f.devices...))
	}))
	mux.HandleFunc("DELETE /api/v1/services/{id}", device(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := f.services[id]; !ok {
			writeJSON(w, http.StatusNotFound, apiError("not_found"))
			return
		}
		delete(f.services, id)
		maps.DeleteFunc(f.mounts, func(_ string, m store.Mount) bool { return m.ServiceID == id })
		f.rev++
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("DELETE /api/v1/mounts/{id}", device(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := f.mounts[r.PathValue("id")]; !ok {
			writeJSON(w, http.StatusNotFound, apiError("not_found"))
			return
		}
		delete(f.mounts, r.PathValue("id"))
		f.rev++
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/v1/services/{id}/token", device(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Field, Value string
			Expiry       time.Time
		}
		id := r.PathValue("id")
		s, ok := f.services[id]
		switch {
		case json.NewDecoder(r.Body).Decode(&b) != nil:
			writeJSON(w, http.StatusBadRequest, apiError("bad_request"))
		case !ok:
			writeJSON(w, http.StatusNotFound, apiError("not_found"))
		case !b.Expiry.After(f.expiry[id]):
			writeJSON(w, http.StatusOK, map[string]string{"kept": "existing"})
		default:
			s.Secrets = maps.Clone(s.Secrets)
			s.Secrets[b.Field] = b.Value
			s.Version++
			f.services[id], f.expiry[id] = s, b.Expiry
			f.rev++
			writeJSON(w, http.StatusOK, map[string]string{"kept": "new"})
		}
	}))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	t.Setenv("DOKKY_API_BASE", f.srv.URL)
	return f
}

// mirrors the server's save()
func save(w http.ResponseWriter, exists bool, version, base int64, taken bool, current any) bool {
	switch {
	case base == 0 && exists, base > 0 && exists && version != base:
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "conflict", "message": "saved elsewhere first"}, "current": current})
	case base > 0 && !exists:
		writeJSON(w, http.StatusNotFound, apiError("not_found"))
	case taken:
		writeJSON(w, http.StatusConflict, apiError("name_taken"))
	default:
		return true
	}
	return false
}

func sortedBy[T any](m map[string]T, name func(T) string) []T {
	return append([]T{}, slices.SortedFunc(maps.Values(m), func(a, b T) int { return cmp.Compare(name(a), name(b)) })...)
}

func apiError(code string) map[string]any {
	return map[string]any{"error": map[string]string{"code": code, "message": "msg " + code}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeAPI) putLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.puts)
}

func (f *fakeAPI) service(id string) store.Service {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.services[id]
}

func (f *fakeAPI) mount(id string) store.Mount {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mounts[id]
}

func (f *fakeAPI) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeAPI) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func (f *fakeAPI) wasRevoked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked
}

type pollReply struct {
	Status  string        `json:"status"`
	Clashes []dsync.Clash `json:"clashes"`
}

func login(t *testing.T, d *Daemon) pollReply {
	t.Helper()
	require.NoError(t, call(t, d, "login.start", nil, nil))
	var r pollReply
	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, "approved", r.Status)
	return r
}

func noOpen(d *Daemon) { d.openURL = func(string) error { return nil } }

func loggedIn(base string) store.Data {
	d := sample("m1")
	d.Account = &store.Account{APIBase: base, DeviceID: "dev1", Token: testToken}
	return d
}

func TestLoginUploadsLocalItems(t *testing.T) {
	api := newFakeAPI(t, "pending", "approved")
	d, _, st := newTestDaemon(t, sample())
	opened := make(chan string, 1)
	d.openURL = func(u string) error { opened <- u; return nil }

	var start struct {
		UserCode  string `json:"userCode"`
		VerifyURL string `json:"verifyUrl"`
		Interval  int    `json:"interval"`
	}
	require.NoError(t, call(t, d, "login.start", nil, &start))
	require.Equal(t, testUserCode, start.UserCode)
	require.Equal(t, api.srv.URL+"/cli/approve?code="+testUserCode, start.VerifyURL)
	require.Equal(t, 2, start.Interval)
	select {
	case u := <-opened:
		require.Equal(t, start.VerifyURL, u)
	case <-time.After(5 * time.Second):
		t.Fatal("browser was not opened")
	}

	var r pollReply
	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, "pending", r.Status)
	require.Nil(t, load(t, st).Account)
	require.Empty(t, api.putLog())

	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, "approved", r.Status)
	require.Empty(t, r.Clashes)
	require.Equal(t, []string{"service nas 0", "mount media 0", "mount docs 0"}, api.putLog())

	saved := load(t, st)
	require.Equal(t, &store.Account{APIBase: api.srv.URL, DeviceID: "dev1", Token: testToken, Rev: `"3"`}, saved.Account, "a pull runs as soon as the login finishes")
	require.Equal(t, int64(1), saved.Services[0].Version)
	require.False(t, saved.Services[0].UpdatedAt.IsZero())
	require.Equal(t, "hunter2-secret", saved.Services[0].Secrets["pass"])
	for _, m := range saved.Mounts {
		require.Equal(t, int64(1), m.Version)
	}
	require.ErrorContains(t, call(t, d, "login.start", nil, nil), "already logged in")
}

func clashAccount(t *testing.T) (*fakeAPI, *Daemon, *fakeEngine, *store.Store, string) {
	t.Helper()
	api := newFakeAPI(t, "approved")
	api.services["r1"] = store.Service{ID: "r1", Name: "nas", Type: "sftp", Options: map[string]string{}, Secrets: map[string]string{"host": "h"}, Version: 2}
	api.mounts["rm1"] = store.Mount{ID: "rm1", Name: "media", ServiceID: "r1", VFS: store.DefaultVFS(), Version: 1}
	point := filepath.Join(t.TempDir(), "media")
	data := sample()
	data.Local["m1"] = store.LocalMount{MountPoint: point}
	d, f, st := newTestDaemon(t, data)
	noOpen(d)
	d.start()
	require.NoError(t, d.mount("media"))
	return api, d, f, st, point
}

func TestLoginClashKeepAccountDeletesLocalCopy(t *testing.T) {
	api, d, f, st, point := clashAccount(t)

	r := login(t, d)
	require.Equal(t, []dsync.Clash{{Kind: "service", Name: "nas", LocalID: "s1", RemoteID: "r1", Local: "webdav, url=http://nas, pass set", Account: "sftp, host set"}}, r.Clashes)
	require.Equal(t, []string{"service nas 0"}, api.putLog(), "mounts of a clashing service wait for it")
	require.True(t, load(t, st).Account.Pending)
	require.True(t, d.status().Pending)
	require.Contains(t, f.mounted, "media")

	var rr struct {
		Clashes []dsync.Clash `json:"clashes"`
	}
	require.NoError(t, call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, &rr))
	require.Equal(t, []dsync.Clash{{Kind: "mount", Name: "media", LocalID: "m1", RemoteID: "rm1", Local: "service nas, path /", Account: "service nas, path /"}}, rr.Clashes)
	require.NotContains(t, f.mounted, "media", "a mount whose service is replaced is unmounted first")
	require.NotContains(t, f.services, "s1")
	saved := load(t, st)
	require.Empty(t, saved.Services)
	require.Len(t, saved.Mounts, 2)
	for _, m := range saved.Mounts {
		require.Equal(t, "r1", m.ServiceID)
	}
	require.Equal(t, "r1", api.mount("m2").ServiceID)
	require.True(t, saved.Account.Pending)

	require.NoError(t, call(t, d, "login.resolve", map[string]string{"kind": "mount", "name": "media", "keep": "account"}, &rr))
	require.Empty(t, rr.Clashes)
	saved = load(t, st)
	require.Equal(t, []string{"m2", "rm1"}, mountIDs(saved), "the pull after login brings the account's media")
	require.NotContains(t, saved.Local, "m1")
	require.Equal(t, point, saved.Local["rm1"].MountPoint, "the account's mount of that name takes over this device's settings")
	require.False(t, saved.Account.Pending)
	require.False(t, d.status().Pending)
	require.Error(t, call(t, d, "login.resolve", map[string]string{"kind": "mount", "name": "media", "keep": "account"}, nil))
}

func TestResolveRefusesWhileMountUploads(t *testing.T) {
	api, d, f, st, _ := clashAccount(t)
	login(t, d)
	f.mu.Lock()
	f.uploads = 2
	f.mu.Unlock()

	for _, keep := range []string{"account", "local"} {
		err := call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": keep}, nil)
		require.ErrorContains(t, err, "2 uploads on media still running")
	}
	require.Contains(t, f.mounted, "media")
	require.Contains(t, f.services, "s1")
	require.Equal(t, []string{"service nas 0"}, api.putLog())
	require.Equal(t, "s1", load(t, st).Mounts[0].ServiceID)
	require.Equal(t, int64(2), api.service("r1").Version)

	f.mu.Lock()
	f.uploads = 0
	f.mu.Unlock()
	require.NoError(t, call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, nil))
}

func setUploads(f *fakeEngine, n int) {
	f.mu.Lock()
	f.uploads = n
	f.mu.Unlock()
}

func requireUnmoved(t *testing.T, f *fakeEngine, st *store.Store) {
	t.Helper()
	require.Contains(t, f.mounted, "media")
	require.Contains(t, f.services, "s1")
	saved := load(t, st)
	require.Equal(t, "s1", saved.Services[0].ID)
	require.Equal(t, "s1", saved.Mounts[0].ServiceID)
	require.True(t, saved.Local["m1"].Mounted)
	require.True(t, saved.Account.Pending)
}

func TestResolveRefusesUploadsDuringNetworkStep(t *testing.T) {
	api, d, f, st, _ := clashAccount(t)
	login(t, d)
	api.set(func() { api.hook = func() { setUploads(f, 1) } })

	err := call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, nil)
	require.ErrorContains(t, err, "1 uploads on media still running")
	requireUnmoved(t, f, st)

	api.set(func() { api.hook = nil })
	setUploads(f, 0)
	require.NoError(t, call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, nil))
}

func TestUploadChecksFailClosed(t *testing.T) {
	_, d, f, st, _ := clashAccount(t)
	login(t, d)
	fillCache(t, d)
	f.mu.Lock()
	f.failStats = errors.New("rc unavailable")
	f.mu.Unlock()

	err := call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, nil)
	require.ErrorContains(t, err, "could not check uploads")
	requireUnmoved(t, f, st)

	err = call(t, d, "logout", map[string]bool{"force": false}, nil)
	require.ErrorContains(t, err, "nothing was removed")
	requireUnmoved(t, f, st)
	require.FileExists(t, filepath.Join(d.cacheDir, "vfs", "s1", "queued.mkv"))
}

func TestLogoutRecheckKeepsCacheForNewUpload(t *testing.T) {
	api := newFakeAPI(t, "approved")
	d, f, st := newTestDaemon(t, loggedIn(api.srv.URL))
	d.start()
	fillCache(t, d)

	logout := handler(d, "logout")
	d.opMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := logout(context.Background(), map[string]bool{"force": false})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	setUploads(f, 1)
	d.opMu.Unlock()

	select {
	case err := <-done:
		t.Fatalf("logout went ahead with a new upload queued: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.FileExists(t, filepath.Join(d.cacheDir, "vfs", "s1", "queued.mkv"))
	require.NotNil(t, load(t, st).Account)
	require.Contains(t, f.mounted, "media")
	require.False(t, api.wasRevoked())

	setUploads(f, 0)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not finish after the upload drained")
	}
	requireWiped(t, d, f, d.cacheDir)
}

func TestLoginClashKeepLocalOverwritesAccount(t *testing.T) {
	api := newFakeAPI(t, "approved")
	api.services["r1"] = store.Service{ID: "r1", Name: "nas", Type: "webdav", Options: map[string]string{"url": "http://old"}, Secrets: map[string]string{}, Version: 3}
	api.mounts["rm9"] = store.Mount{ID: "rm9", Name: "photos", ServiceID: "r1", VFS: store.DefaultVFS(), Version: 1}
	data := sample()
	data.Mounts = data.Mounts[:1]
	data.Local["m1"] = store.LocalMount{MountPoint: "/elsewhere"}
	d, f, st := newTestDaemon(t, data)
	noOpen(d)
	d.start()

	r := login(t, d)
	require.Len(t, r.Clashes, 1)
	require.NoError(t, call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "local"}, nil))

	r1 := api.service("r1")
	require.Equal(t, int64(4), r1.Version)
	require.Equal(t, "http://nas", r1.Options["url"])
	require.Equal(t, "hunter2-secret", r1.Secrets["pass"])
	require.Equal(t, "r1", api.mount("rm9").ServiceID, "the account's other mounts keep their service")
	require.Equal(t, "r1", api.mount("m1").ServiceID)

	saved := load(t, st)
	require.Len(t, saved.Services, 1)
	require.Equal(t, "r1", saved.Services[0].ID)
	require.Equal(t, int64(4), saved.Services[0].Version)
	require.Equal(t, "r1", saved.Mounts[0].ServiceID)
	require.Equal(t, "/elsewhere", saved.Local["m1"].MountPoint)
	require.False(t, saved.Account.Pending)
	require.Contains(t, f.services, "r1")
	require.NotContains(t, f.services, "s1")
}

func TestLoginRetriedUploadCountsAsDone(t *testing.T) {
	api := newFakeAPI(t, "approved")
	local := func() store.Data {
		data := sample()
		data.Mounts = data.Mounts[:1]
		return data
	}
	sent := local().Services[0]
	sent.Version = 1
	api.services["s1"] = sent
	d, _, st := newTestDaemon(t, local())
	noOpen(d)

	require.Empty(t, login(t, d).Clashes)
	saved := load(t, st)
	require.Equal(t, int64(1), saved.Services[0].Version)
	require.False(t, saved.Account.Pending)

	changed := store.Service{ID: "s1", Name: "nas", Type: "webdav", Options: map[string]string{"url": "http://changed"}, Version: 2}
	api.set(func() { api.services["s1"] = changed })
	d2, f2, st2 := newTestDaemon(t, local())
	noOpen(d2)
	require.Empty(t, login(t, d2).Clashes)
	saved = load(t, st2)
	require.Equal(t, "http://changed", saved.Services[0].Options["url"], "the account's copy wins, as in a pull")
	require.Equal(t, int64(2), saved.Services[0].Version)
	require.False(t, saved.Account.Pending)
	require.Equal(t, "http://changed", f2.services["s1"].Options["url"])
}

func TestLoginResumeAfterAccountChange(t *testing.T) {
	api := newFakeAPI(t, "approved")
	api.fail["media"] = "unavailable"
	data := sample()
	data.Mounts = data.Mounts[:1]
	d, _, st := newTestDaemon(t, data)
	noOpen(d)
	require.NoError(t, call(t, d, "login.start", nil, nil))
	require.ErrorContains(t, call(t, d, "login.poll", nil, nil), "media")
	require.True(t, load(t, st).Account.Pending)

	api.set(func() {
		delete(api.fail, "media")
		s := api.services["s1"]
		s.Options, s.Version = map[string]string{"url": "http://edited"}, 2
		api.services["s1"] = s
	})
	var start struct {
		Resume bool `json:"resume"`
	}
	require.NoError(t, call(t, d, "login.start", nil, &start))
	require.True(t, start.Resume)
	var r pollReply
	require.NoError(t, call(t, d, "login.poll", nil, &r))
	require.Equal(t, "approved", r.Status)
	require.Empty(t, r.Clashes)
	saved := load(t, st)
	require.False(t, saved.Account.Pending)
	require.Equal(t, "http://edited", saved.Services[0].Options["url"])
	require.Equal(t, int64(1), saved.Mounts[0].Version)
}

func TestFailedUploadLeavesNoClashToResolve(t *testing.T) {
	api := newFakeAPI(t, "approved")
	api.services["r1"] = store.Service{ID: "r1", Name: "nas", Type: "sftp", Version: 1}
	api.fail["box"] = "unavailable"
	data := sample()
	data.Services = append(data.Services, store.Service{ID: "s2", Name: "box", Type: "webdav", Options: map[string]string{"url": "http://box"}})
	d, _, st := newTestDaemon(t, data)
	noOpen(d)
	require.NoError(t, call(t, d, "login.start", nil, nil))
	require.ErrorContains(t, call(t, d, "login.poll", nil, nil), "box")
	require.ErrorContains(t, call(t, d, "login.resolve", map[string]string{"kind": "service", "name": "nas", "keep": "account"}, nil), "no service")
	saved := load(t, st)
	require.True(t, saved.Account.Pending)
	require.Len(t, saved.Services, 2)
}

func TestLoginDeniedOrExpiredStoresNothing(t *testing.T) {
	for _, status := range []string{"denied", "expired"} {
		t.Run(status, func(t *testing.T) {
			newFakeAPI(t, status)
			d, _, st := newTestDaemon(t, sample())
			noOpen(d)
			require.NoError(t, call(t, d, "login.start", nil, nil))
			var r pollReply
			require.NoError(t, call(t, d, "login.poll", nil, &r))
			require.Equal(t, status, r.Status)
			require.Nil(t, load(t, st).Account)
			require.Nil(t, d.data.Account)
			require.ErrorContains(t, call(t, d, "login.poll", nil, nil), "no login")
		})
	}
}

func handler(d *Daemon, method string) func(ctx context.Context, params any) (any, error) {
	h := d.handlers(nil)[method]
	return func(ctx context.Context, params any) (any, error) {
		raw, _ := json.Marshal(params)
		return h(ctx, raw)
	}
}

func fillCache(t *testing.T, d *Daemon) {
	t.Helper()
	for _, dir := range []string{"vfs", "vfsMeta"} {
		p := filepath.Join(d.cacheDir, dir, "s1", "queued.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
}

func requireWiped(t *testing.T, d *Daemon, f *fakeEngine, dir string) {
	t.Helper()
	require.NoDirExists(t, filepath.Join(d.cacheDir, "vfs"))
	require.NoDirExists(t, filepath.Join(d.cacheDir, "vfsMeta"))
	require.Empty(t, f.mounted)
	require.Empty(t, f.services)
	require.NoFileExists(t, filepath.Join(dir, "store.enc"))
	st := d.status()
	require.False(t, st.LoggedIn)
	require.Empty(t, st.Mounts)
	require.NoError(t, call(t, d, "service.put", store.Service{ID: "n1", Name: "fresh", Type: "webdav", Options: map[string]string{"url": "http://x"}}, nil))
	reopened, err := store.Open(dir, store.SystemKeyring(dir))
	require.NoError(t, err)
	got, err := reopened.Load()
	require.NoError(t, err)
	require.Nil(t, got.Account)
	require.Len(t, got.Services, 1)
	require.Equal(t, "n1", got.Services[0].ID)
	require.True(t, got.Settings.AutoUpdate)
}

func TestLogoutWaitsForUploads(t *testing.T) {
	api := newFakeAPI(t, "approved")
	d, f, st := newTestDaemon(t, loggedIn(api.srv.URL))
	d.start()
	fillCache(t, d)
	require.Contains(t, f.mounted, "media")
	f.mu.Lock()
	f.uploads = 2
	f.mu.Unlock()
	logout := handler(d, "logout")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := logout(ctx, map[string]bool{"force": false})
	require.ErrorContains(t, err, "2 uploads")
	require.NotNil(t, load(t, st).Account)
	require.FileExists(t, filepath.Join(d.cacheDir, "vfs", "s1", "queued.mkv"))
	require.Contains(t, f.mounted, "media")
	require.False(t, api.wasRevoked())

	done := make(chan error, 1)
	go func() {
		_, err := logout(context.Background(), map[string]bool{"force": false})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("logout returned with uploads waiting: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	f.mu.Lock()
	f.uploads = 0
	f.mu.Unlock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not finish after uploads drained")
	}
	require.True(t, api.wasRevoked())
	requireWiped(t, d, f, d.cacheDir)
}

func TestLogoutForceSkipsWait(t *testing.T) {
	api := newFakeAPI(t, "approved")
	d, f, _ := newTestDaemon(t, loggedIn(api.srv.URL))
	d.start()
	fillCache(t, d)
	f.mu.Lock()
	f.uploads = 3
	f.mu.Unlock()
	within(t, 5*time.Second, func() {
		require.NoError(t, call(t, d, "logout", map[string]bool{"force": true}, nil))
	})
	require.True(t, api.wasRevoked())
	requireWiped(t, d, f, d.cacheDir)
}

func TestLogoutOfflineRevokeStillWipes(t *testing.T) {
	api := newFakeAPI(t, "approved")
	api.srv.Close()
	d, f, _ := newTestDaemon(t, loggedIn(api.srv.URL))
	d.start()
	fillCache(t, d)
	var r struct {
		Warning string `json:"warning"`
	}
	require.NoError(t, call(t, d, "logout", map[string]bool{"force": false}, &r))
	require.Contains(t, r.Warning, api.srv.URL+"/devices")
	requireWiped(t, d, f, d.cacheDir)
	require.ErrorContains(t, call(t, d, "logout", nil, nil), "not logged in")
}

func TestLoginTokenNeverInLogsOrErrors(t *testing.T) {
	ctx := context.Background()
	prevLevel, prevDefault := fs.GetConfig(ctx).LogLevel, slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		fs.GetConfig(ctx).LogLevel = prevLevel
		fs.SetLogger(prevDefault.Handler())
		logx.SetSecrets(nil)
	})
	path := filepath.Join(t.TempDir(), "dokky.log")

	api := newFakeAPI(t, "approved")
	api.fail["nas"] = "rejected Bearer " + testToken
	d, _, _ := newTestDaemon(t, sample())
	d.log = logx.New(path)
	noOpen(d)
	require.NoError(t, call(t, d, "login.start", nil, nil))
	err := call(t, d, "login.poll", nil, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), testToken)
	d.log.Error("request failed", "err", "Authorization: Bearer "+testToken)

	api.srv.Close()
	require.NoError(t, call(t, d, "logout", map[string]bool{"force": true}, nil))

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(b), testToken)
	require.Contains(t, string(b), "***")

	d2, _, _ := newTestDaemon(t, loggedIn("http://127.0.0.1:1"))
	d2.start()
	require.NotContains(t, logx.Redact("token "+testToken), testToken)
}

func mountIDs(d store.Data) []string {
	var ids []string
	for _, m := range d.Mounts {
		ids = append(ids, m.ID)
	}
	return ids
}
