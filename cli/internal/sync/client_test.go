package sync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/buildinfo"
	"github.com/itzzritik/dokky/cli/internal/store"
	"github.com/stretchr/testify/require"
)

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Token: "dky_tok"}
}

func apiErr(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "msg " + code}})
}

func TestPullNotModifiedSendsQuotedETag(t *testing.T) {
	var got string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusNotModified)
	})
	snap, nm, err := c.Pull(context.Background(), `"12"`)
	require.NoError(t, err)
	require.True(t, nm)
	require.Nil(t, snap)
	require.Equal(t, `"12"`, got)
}

func TestPullSnapshot(t *testing.T) {
	var path, inm string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, inm = r.URL.Path, r.Header.Get("If-None-Match")
		w.Header().Set("ETag", `"13"`)
		_, _ = w.Write([]byte(`{"rev":13,"services":[{"id":"s1","name":"nas","type":"sftp","options":{"host":"h"},"secrets":{"pass":"p"},"version":2,"updatedAt":"2026-10-30T10:00:00.000Z"}],"mounts":[{"id":"m1","name":"media","serviceId":"s1","remotePath":"/","vfs":{"cacheMode":"full","readChunkStreams":4,"fastFingerprint":true},"version":1,"updatedAt":"2026-10-30T10:00:00.000Z"}]}`))
	})
	snap, nm, err := c.Pull(context.Background(), "")
	require.NoError(t, err)
	require.False(t, nm)
	require.Equal(t, "/api/v1/sync", path)
	require.Empty(t, inm)
	require.Equal(t, `"13"`, snap.Rev)
	require.Equal(t, "p", snap.Services[0].Secrets["pass"])
	require.Equal(t, int64(2), snap.Services[0].Version)
	require.Equal(t, 2026, snap.Services[0].UpdatedAt.Year())
	require.Equal(t, "s1", snap.Mounts[0].ServiceID)
	require.Equal(t, 4, snap.Mounts[0].VFS.ReadChunkStreams)
}

func TestPullMalformedIsError(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"1"`)
		_, _ = w.Write([]byte(`{"rev":1,"services":[{"id":`))
	})
	snap, _, err := c.Pull(context.Background(), "")
	require.Error(t, err)
	require.Nil(t, snap)
}

func TestHeadersOnAuthedRequests(t *testing.T) {
	var auth, ver string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth, ver = r.Header.Get("Authorization"), r.Header.Get("X-Dokky-Version")
		_, _ = w.Write([]byte(`[]`))
	})
	_, err := c.Devices(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer dky_tok", auth)
	require.Equal(t, buildinfo.Version, ver)
}

func TestVersionOverride(t *testing.T) {
	var ver string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		ver = r.Header.Get("X-Dokky-Version")
		_, _ = w.Write([]byte(`[]`))
	})
	c.Version = "9.9.9"
	_, err := c.Devices(context.Background())
	require.NoError(t, err)
	require.Equal(t, "9.9.9", ver)
}

func TestLoginCallsHaveNoAuthorization(t *testing.T) {
	var paths, auths, vers []string
	var startBody map[string]string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		vers = append(vers, r.Header.Get("X-Dokky-Version"))
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "start") {
			_ = json.NewDecoder(r.Body).Decode(&startBody)
			_, _ = w.Write([]byte(`{"deviceCode":"dc","userCode":"BCDF-GHJK","verifyUrl":"https://x/y","interval":2,"expiresIn":600}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"approved","token":"dky_new","deviceId":"d1"}`))
	})
	c.Token = "dky_should_not_be_sent"
	st, err := c.LoginStart(context.Background(), DeviceInfo{DeviceName: "mac", OS: "darwin", Arch: "arm64", Version: "1"})
	require.NoError(t, err)
	require.Equal(t, "BCDF-GHJK", st.UserCode)
	require.Equal(t, 2, st.Interval)
	p, err := c.LoginPoll(context.Background(), "dc")
	require.NoError(t, err)
	require.Equal(t, LoginPoll{Status: "approved", Token: "dky_new", DeviceID: "d1"}, p)
	require.Equal(t, []string{"/api/v1/cli/login/start", "/api/v1/cli/login/poll"}, paths)
	require.Equal(t, []string{"", ""}, auths)
	require.NotContains(t, vers, "")
	require.Equal(t, "mac", startBody["deviceName"])
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
		is     error
	}{
		{"revoked", 401, "device_revoked", ErrRevoked},
		{"unauthorized", 401, "unauthorized", ErrUnauthorized},
		{"name taken", 409, "name_taken", ErrNameTaken},
		{"not found", 404, "not_found", ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, func(w http.ResponseWriter, r *http.Request) { apiErr(w, tc.status, tc.code) })
			_, _, err := c.Pull(context.Background(), "")
			require.ErrorIs(t, err, tc.is)
			require.ErrorIs(t, c.DeleteMount(context.Background(), "m"), tc.is)
		})
	}
}

func TestConflictCarriesCurrent(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"m"},"current":{"id":"s1","version":5}}`))
	})
	_, err := c.PutService(context.Background(), store.Service{ID: "s1"}, 4)
	var ce *ConflictError
	require.True(t, errors.As(err, &ce))
	require.JSONEq(t, `{"id":"s1","version":5}`, string(ce.Current))
}

func TestOtherErrorHasStatusAndCodeButNoBody(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { apiErr(w, 500, "boom") })
	_, err := c.PutService(context.Background(), store.Service{ID: "s1", Secrets: map[string]string{"pass": "hunter2"}}, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "500")
	require.Contains(t, err.Error(), "boom")
	require.NotContains(t, err.Error(), "hunter2")
}

func TestPutServiceBodyAndReply(t *testing.T) {
	var method, path string
	var body map[string]any
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"s1","name":"nas","type":"sftp","options":{},"secrets":{},"version":4,"updatedAt":"2026-10-30T10:00:00.000Z"}`))
	})
	got, err := c.PutService(context.Background(), store.Service{ID: "s1", Name: "nas", Type: "sftp", Options: map[string]string{"host": "h"}, Secrets: map[string]string{"pass": "p"}}, 3)
	require.NoError(t, err)
	require.Equal(t, int64(4), got.Version)
	require.Equal(t, http.MethodPut, method)
	require.Equal(t, "/api/v1/services/s1", path)
	require.Equal(t, map[string]any{"name": "nas", "type": "sftp", "options": map[string]any{"host": "h"}, "secrets": map[string]any{"pass": "p"}, "baseVersion": float64(3)}, body)
}

func TestPutServiceNilMapsSendEmptyObjects(t *testing.T) {
	var raw string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		_, _ = w.Write([]byte(`{"id":"s1","version":1,"updatedAt":"2026-10-30T10:00:00.000Z"}`))
	})
	_, err := c.PutService(context.Background(), store.Service{ID: "s1", Name: "n", Type: "t"}, 0)
	require.NoError(t, err)
	require.Contains(t, raw, `"options":{}`)
	require.Contains(t, raw, `"secrets":{}`)
	require.NotContains(t, raw, "null")
}

func TestPutMountBodyAndDelete(t *testing.T) {
	var paths []string
	var body map[string]json.RawMessage
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"m1","name":"media","serviceId":"s1","remotePath":"/","vfs":{},"version":1,"updatedAt":"2026-10-30T10:00:00.000Z"}`))
	})
	m, err := c.PutMount(context.Background(), store.Mount{ID: "m1", Name: "media", ServiceID: "s1", RemotePath: "/", VFS: store.DefaultVFS()}, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), m.Version)
	require.NoError(t, c.DeleteMount(context.Background(), "m1"))
	require.Equal(t, []string{"/api/v1/mounts/m1", "/api/v1/mounts/m1"}, paths)
	require.Len(t, body, 5)
	for _, k := range []string{"name", "serviceId", "remotePath", "vfs", "baseVersion"} {
		require.Contains(t, body, k)
	}
}

func TestDeleteServiceRejectsNon204(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	require.Error(t, c.DeleteService(context.Background(), "s1"))
}

func TestWriteTokenBody(t *testing.T) {
	var method, path string
	var body map[string]string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"kept":"new"}`))
	})
	require.NoError(t, c.WriteToken(context.Background(), "s1", "token", "v", time.Date(2026, 10, 30, 15, 30, 0, 0, time.FixedZone("IST", 19800))))
	require.Equal(t, http.MethodPost, method)
	require.Equal(t, "/api/v1/services/s1/token", path)
	require.Equal(t, map[string]string{"field": "token", "value": "v", "expiry": "2026-10-30T10:00:00Z"}, body)
}

func TestDevicesAndRevoke(t *testing.T) {
	var delPath string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			delPath = r.URL.Path
			w.WriteHeader(204)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"d1","name":"mac","os":"darwin","arch":"arm64","version":"1.0.0","lastSeenAt":"2026-10-30T10:00:00.000Z","createdAt":"2026-10-29T10:00:00.000Z","current":true}]`))
	})
	ds, err := c.Devices(context.Background())
	require.NoError(t, err)
	require.True(t, ds[0].Current)
	require.Equal(t, "mac", ds[0].Name)
	require.NoError(t, c.Revoke(context.Background(), "d2"))
	require.Equal(t, "/api/v1/devices/d2", delPath)
}

func TestOversizedBodyIsError(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"1"`)
		_, _ = w.Write([]byte(`{"rev":1,"services":[],"mounts":[],"pad":"`))
		chunk := strings.Repeat("a", 1<<20)
		for range 9 {
			_, _ = w.Write([]byte(chunk))
		}
		_, _ = w.Write([]byte(`"}`))
	})
	snap, _, err := c.Pull(context.Background(), "")
	require.Error(t, err)
	require.Nil(t, snap)
}

func TestPullRejectsIncompleteSnapshots(t *testing.T) {
	for name, tc := range map[string]struct{ etag, body string }{
		"empty object": {`"1"`, `{}`},
		"null":         {`"1"`, `null`},
		"no mounts":    {`"1"`, `{"services":[]}`},
		"no services":  {`"1"`, `{"mounts":[]}`},
		"missing etag": {``, `{"services":[],"mounts":[]}`},
	} {
		t.Run(name, func(t *testing.T) {
			c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.etag != "" {
					w.Header().Set("ETag", tc.etag)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			snap, _, err := c.Pull(context.Background(), "")
			require.Error(t, err)
			require.Nil(t, snap)
		})
	}
}

func TestPullAcceptsEmptyAccount(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"4"`)
		_, _ = w.Write([]byte(`{"rev":4,"services":[],"mounts":[]}`))
	})
	snap, _, err := c.Pull(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, `"4"`, snap.Rev)
	require.Empty(t, snap.Services)
}

func TestNonJSONErrorFallsBackToStatusText(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	_, _, err := c.Pull(context.Background(), "")
	require.ErrorContains(t, err, "502")
	require.ErrorContains(t, err, "Bad Gateway")
	require.NotContains(t, err.Error(), "<html>")
}
