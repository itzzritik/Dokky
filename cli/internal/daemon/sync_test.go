package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/ipc"
	"github.com/itzzritik/dokky/cli/internal/logx"
	"github.com/itzzritik/dokky/cli/internal/store"
	dsync "github.com/itzzritik/dokky/cli/internal/sync"
	"github.com/stretchr/testify/require"
)

const testOAuth = `{"access_token":"a2","token_type":"Bearer","refresh_token":"r2","expiry":"2026-11-01T10:00:00Z"}`

// the account and this device hold the same items; media is mounted
func synced(t *testing.T) (*fakeAPI, *Daemon, *fakeEngine, *store.Store) {
	t.Helper()
	api := newFakeAPI(t)
	data := loggedIn(api.srv.URL)
	for i := range data.Services {
		data.Services[i].Version = 1
		api.services[data.Services[i].ID] = data.Services[i]
	}
	for i := range data.Mounts {
		data.Mounts[i].Version = 1
		api.mounts[data.Mounts[i].ID] = data.Mounts[i]
	}
	d, f, st := newTestDaemon(t, data)
	d.start()
	require.Contains(t, f.mounted, "media")
	return api, d, f, st
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var ce ipc.CodedError
	require.True(t, errors.As(err, &ce), "error %v has no code", err)
	return ce.Code()
}

func TestPullAppliesAndSaves(t *testing.T) {
	api, d, f, st := synced(t)
	api.set(func() {
		api.services["s2"] = store.Service{ID: "s2", Name: "box", Type: "webdav", Options: map[string]string{"url": "http://box"}, Secrets: map[string]string{"pass": "box-secret-value"}, Version: 1}
		api.mounts["m3"] = store.Mount{ID: "m3", Name: "photos", ServiceID: "s2", VFS: store.DefaultVFS(), Version: 1}
		m1 := api.mounts["m1"]
		m1.RemotePath, m1.Version = "movies", 2
		api.mounts["m1"] = m1
		delete(api.mounts, "m2")
		api.rev = 5
	})
	f.mu.Lock()
	mountedAt := f.mountedAt
	f.mu.Unlock()

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	saved := load(t, st)
	require.Equal(t, []string{"m1", "m3"}, mountIDs(saved))
	require.Equal(t, "movies", saved.Mounts[0].RemotePath)
	require.Len(t, saved.Services, 2)
	require.Equal(t, `"5"`, saved.Account.Rev)
	require.True(t, saved.Local["m1"].Mounted)
	require.Equal(t, "box-secret-value", f.services["s2"].Secrets["pass"])
	require.Contains(t, f.mounted, "media")
	require.Equal(t, mountedAt, f.mountedAt, "a change to a live mount waits for its next mount")
	require.True(t, d.mountStatus("media").RemountToApply)
	require.False(t, d.mountStatus("photos").RemountToApply)
	require.NotContains(t, logx.Redact("pass box-secret-value"), "box-secret-value")

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Equal(t, []string{"GET /api/v1/sync", "GET /api/v1/sync 304"}, api.requests())
	require.Equal(t, saved, load(t, st))

	require.NoError(t, call(t, d, "unmount", map[string]string{"name": "media"}, nil))
	require.NoError(t, call(t, d, "mount", map[string]string{"name": "media"}, nil))
	require.False(t, d.mountStatus("media").RemountToApply)
}

func TestPullIgnoresBadResponses(t *testing.T) {
	cases := map[string]struct {
		h                  http.HandlerFunc
		offline, signedOut bool
	}{
		"500": {h: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusInternalServerError, apiError("internal"))
		}},
		"timeout": {offline: true, h: func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }},
		"malformed": {h: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("ETag", `"9"`)
			_, _ = w.Write([]byte(`{"rev":9,"services":[{"id":"x","name":`))
		}},
		"plain 401": {signedOut: true, h: func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusUnauthorized, apiError("unauthorized"))
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(c.h)
			t.Cleanup(srv.Close)
			d, f, st := newTestDaemon(t, loggedIn(srv.URL))
			d.start()
			fillCache(t, d)
			before := load(t, st)

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			d.syncNow(ctx)

			require.Equal(t, before, load(t, st))
			require.Contains(t, f.mounted, "media")
			require.Contains(t, f.services, "s1")
			require.FileExists(t, filepath.Join(d.cacheDir, "vfs", "s1", "queued.mkv"))
			s := d.status()
			require.True(t, s.LoggedIn)
			require.Equal(t, c.offline, s.Offline)
			require.Equal(t, c.signedOut, s.SignedOut)
		})
	}
}

func TestRevokedWipesEverything(t *testing.T) {
	for _, via := range []string{"pull", "save", "token write-back", "login upload"} {
		t.Run(via, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusUnauthorized, apiError("device_revoked"))
			}))
			t.Cleanup(srv.Close)
			data := loggedIn(srv.URL)
			data.Account.Pending = via == "login upload"
			d, f, _ := newTestDaemon(t, data)
			d.start()
			fillCache(t, d)
			setUploads(f, 2)

			switch via {
			case "pull":
				require.NoError(t, call(t, d, "sync.now", nil, nil))
			case "save":
				err := call(t, d, "mount.put", store.Mount{ID: "m9", Name: "new", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)
				require.Equal(t, "revoked", codeOf(t, err))
			case "login upload":
				require.Equal(t, "revoked", codeOf(t, call(t, d, "login.poll", nil, nil)))
			default:
				d.onToken("s1", testOAuth)
				require.Eventually(t, func() bool {
					f.mu.Lock()
					defer f.mu.Unlock()
					return len(f.services) == 0
				}, 5*time.Second, time.Millisecond)
			}
			requireWiped(t, d, f, d.cacheDir)
		})
	}
}

func TestRevokedWipeForcesBusyMount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, apiError("device_revoked"))
	}))
	t.Cleanup(srv.Close)
	d, f, _ := newTestDaemon(t, loggedIn(srv.URL))
	d.start()
	point := d.mountStatus("media").MountPoint
	f.mu.Lock()
	f.failUnmount = 1 << 20
	f.mu.Unlock()

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	f.mu.Lock()
	require.Contains(t, f.forced, point)
	require.False(t, f.os[point])
	f.mu.Unlock()
	require.False(t, d.status().LoggedIn)
	require.NoFileExists(t, filepath.Join(d.cacheDir, "store.enc"))
}

func TestOwnWriteBackPullsFreshVersion(t *testing.T) {
	api, d, _, st := synced(t)
	d.onToken("s1", testOAuth)
	require.Eventually(t, func() bool { return load(t, st).Services[0].Version == 2 }, 5*time.Second, time.Millisecond)
	require.NoError(t, call(t, d, "service.put", map[string]any{"id": "s1", "name": "nas", "type": "webdav", "options": map[string]string{"url": "http://edit"}, "version": 2, "editOf": "s1"}, nil))
	require.Equal(t, int64(3), api.service("s1").Version)
}

func TestTokenWriteBackRetriesWhenOffline(t *testing.T) {
	api, d, f, st := synced(t)
	api.set(func() { api.down = true })

	d.onToken("s1", testOAuth)
	require.Equal(t, testOAuth, load(t, st).Services[0].Secrets["token"])
	require.Eventually(t, func() bool { return d.status().Offline }, 5*time.Second, time.Millisecond)
	d.syncNow(context.Background())
	require.Empty(t, api.requests())

	api.set(func() { api.down = false })
	d.syncNow(context.Background())
	require.Equal(t, []string{"POST /api/v1/services/s1/token", "GET /api/v1/sync"}, api.requests())
	require.Equal(t, testOAuth, api.service("s1").Secrets["token"])
	api.set(func() { require.Equal(t, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), api.expiry["s1"].UTC()) })
	saved := load(t, st)
	require.Equal(t, testOAuth, saved.Services[0].Secrets["token"])
	require.Equal(t, int64(2), saved.Services[0].Version)
	require.Equal(t, testOAuth, f.services["s1"].Secrets["token"])
	require.False(t, d.status().Offline)
}

func TestOnTokenIgnoresUnknownIDs(t *testing.T) {
	api, d, _, st := synced(t)
	before := load(t, st)
	d.onToken("configure-throwaway", testOAuth)
	d.onToken("s1", "not a token")
	require.Equal(t, before, load(t, st))
	d.syncNow(context.Background())
	require.Equal(t, []string{"GET /api/v1/sync"}, api.requests(), "nothing was queued for write-back")
}

func TestSaveConflictSurfacesCurrent(t *testing.T) {
	api, d, f, st := synced(t)
	newer := api.service("s1")
	newer.Options, newer.Secrets, newer.Version = map[string]string{"url": "http://moved"}, map[string]string{"pass": "their-new-secret"}, 3
	api.set(func() { api.services["s1"] = newer })
	before := load(t, st)

	err := call(t, d, "service.put", map[string]any{"id": "s1", "name": "nas", "type": "webdav", "options": map[string]string{"url": "http://edit"}, "version": 1, "editOf": "s1"}, nil)
	require.Equal(t, "conflict", codeOf(t, err))
	require.NotContains(t, err.Error(), "their-new-secret")
	var cur store.Service
	require.NoError(t, json.Unmarshal([]byte(err.Error()), &cur))
	require.Equal(t, "http://moved", cur.Options["url"])
	require.Equal(t, int64(3), cur.Version)
	require.Equal(t, map[string]string{"pass": ""}, cur.Secrets)

	require.Equal(t, before, load(t, st))
	require.Equal(t, "http://nas", f.services["s1"].Options["url"])
}

func TestSaveRetriedConflictCountsAsSaved(t *testing.T) {
	api, d, f, st := synced(t)
	landed := api.service("s1")
	landed.Options, landed.Version = map[string]string{"url": "http://edit"}, 2
	api.set(func() { api.services["s1"] = landed })

	require.NoError(t, call(t, d, "service.put", map[string]any{"id": "s1", "name": "nas", "type": "webdav", "options": map[string]string{"url": "http://edit"}, "version": 1, "editOf": "s1"}, nil))
	saved := load(t, st)
	require.Equal(t, int64(2), saved.Services[0].Version)
	require.Equal(t, "http://edit", saved.Services[0].Options["url"])
	require.Equal(t, "http://edit", f.services["s1"].Options["url"])
}

func TestSavesGoToServerFirst(t *testing.T) {
	api, d, f, st := synced(t)
	var id struct {
		ID string `json:"id"`
	}
	require.NoError(t, call(t, d, "mount.put", store.Mount{Name: "photos", ServiceID: "s1", VFS: store.DefaultVFS()}, &id))
	require.Equal(t, int64(1), api.mount(id.ID).Version)
	saved := load(t, st)
	require.Equal(t, int64(1), saved.Mounts[2].Version)
	require.Equal(t, "photos", saved.Mounts[2].Name)

	require.NoError(t, call(t, d, "mount.delete", map[string]string{"id": "m2"}, nil))
	require.Empty(t, api.mount("m2").ID)
	require.Equal(t, []string{"m1", id.ID}, mountIDs(load(t, st)))

	err := call(t, d, "mount.put", store.Mount{ID: "m7", Name: "media", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)
	require.ErrorContains(t, err, "already exists")
	api.set(func() {
		api.mounts["other"] = store.Mount{ID: "other", Name: "music", ServiceID: "s1", Version: 1}
	})
	err = call(t, d, "mount.put", store.Mount{ID: "m8", Name: "music", ServiceID: "s1", VFS: store.DefaultVFS()}, nil)
	require.Equal(t, "name_taken", codeOf(t, err))

	api.set(func() { api.down = true })
	before := load(t, st)
	for method, params := range map[string]any{
		"service.put":    store.Service{ID: "s5", Name: "box", Type: "webdav", Options: map[string]string{"url": "http://box"}},
		"service.delete": map[string]string{"id": "s1"},
		"mount.put":      store.Mount{ID: "m5", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()},
		"mount.delete":   map[string]string{"id": "m1"},
	} {
		require.Equal(t, "offline", codeOf(t, call(t, d, method, params, nil)), method)
	}
	require.Equal(t, before, load(t, st), "nothing is saved locally while offline")
	require.Contains(t, f.mounted, "media")
	require.Contains(t, f.services, "s1")
	require.NotContains(t, f.services, "s5")

	api.set(func() { api.down = false })
	require.NoError(t, call(t, d, "service.delete", map[string]string{"id": "s1"}, nil))
	require.Empty(t, api.service("s1").ID)
	require.Empty(t, load(t, st).Services)
	require.Empty(t, f.mounted)
}

func TestSavesRefusedWhileLoginPending(t *testing.T) {
	api := newFakeAPI(t)
	data := loggedIn(api.srv.URL)
	data.Account.Pending = true
	d, _, st := newTestDaemon(t, data)
	for method, params := range map[string]any{
		"service.put":    store.Service{ID: "s5", Name: "box", Type: "webdav", Options: map[string]string{"url": "http://box"}},
		"service.delete": map[string]string{"id": "s1"},
		"mount.put":      store.Mount{ID: "m5", Name: "later", ServiceID: "s1", VFS: store.DefaultVFS()},
		"mount.delete":   map[string]string{"id": "m1"},
	} {
		require.Equal(t, "login_pending", codeOf(t, call(t, d, method, params, nil)), method)
	}
	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.Equal(t, data, load(t, st))
	require.Empty(t, api.requests())
}

func TestRemovedMountKeepsServiceUntilUnmounted(t *testing.T) {
	api, d, f, st := synced(t)
	api.set(func() {
		delete(api.services, "s1")
		delete(api.mounts, "m1")
		delete(api.mounts, "m2")
		api.rev = 4
	})

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	saved := load(t, st)
	require.Equal(t, []string{"m1"}, mountIDs(saved))
	require.True(t, saved.Local["m1"].RemovedRemotely)
	require.Len(t, saved.Services, 1)
	require.Contains(t, f.services, "s1")
	require.Contains(t, f.mounted, "media")
	require.Equal(t, stateRemoved, d.mountStatus("media").State)

	require.NoError(t, call(t, d, "unmount", map[string]string{"name": "media"}, nil))
	saved = load(t, st)
	require.Empty(t, saved.Mounts)
	require.NotContains(t, saved.Local, "m1")
	require.Empty(t, saved.Account.Rev)
	require.Contains(t, f.services, "s1", "the next pull decides the service")

	require.NoError(t, call(t, d, "sync.now", nil, nil))
	require.NotContains(t, f.services, "s1")
	require.Empty(t, load(t, st).Services)
}

func TestDevicesListAndRevoke(t *testing.T) {
	api, d, f, _ := synced(t)
	api.set(func() {
		api.devices = []dsync.Device{{ID: "dev1", Name: "mac", OS: "darwin", Current: true}, {ID: "dev2", Name: "office-pc", OS: "windows"}}
	})
	var list []dsync.Device
	require.NoError(t, call(t, d, "devices.list", nil, &list))
	require.Len(t, list, 2)
	require.True(t, list[0].Current)

	require.NoError(t, call(t, d, "devices.revoke", map[string]string{"id": "dev2"}, nil))
	api.set(func() { require.Equal(t, []string{"dev2"}, api.revokes) })
	require.True(t, d.status().LoggedIn)

	fillCache(t, d)
	require.NoError(t, call(t, d, "devices.revoke", map[string]string{"id": "self"}, nil))
	require.True(t, api.wasRevoked())
	requireWiped(t, d, f, d.cacheDir)
	require.ErrorContains(t, call(t, d, "devices.list", nil, nil), "not logged in")
}
