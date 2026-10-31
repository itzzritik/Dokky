package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoopFlushesTokensBeforePull(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	failing := true
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/v1/sync" {
			seen = append(seen, "pull "+r.Header.Get("If-None-Match"))
			w.Header().Set("ETag", `"2"`)
			_, _ = w.Write([]byte(`{"rev":2,"services":[],"mounts":[]}`))
			return
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		seen = append(seen, r.URL.Path+" "+b["field"]+" "+b["value"]+" "+b["expiry"])
		switch {
		case r.URL.Path == "/api/v1/services/gone/token":
			apiErr(w, http.StatusNotFound, "not_found")
		case failing:
			apiErr(w, http.StatusServiceUnavailable, "unavailable")
		default:
			_, _ = w.Write([]byte(`{"kept":"new"}`))
		}
	})
	ctx := context.Background()
	exp := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	var logged bytes.Buffer
	l := Loop{Log: slog.New(slog.NewTextHandler(&logged, nil))}
	l.Token("s1", "old", exp)
	l.Token("s1", "new", exp.Add(time.Hour))
	require.True(t, l.Pending())

	snap, err := l.Tick(ctx, c, `"1"`)
	require.ErrorContains(t, err, "token write-back for service s1 holds back pulls")
	require.Nil(t, snap)
	require.True(t, l.Pending())
	require.Equal(t, []string{"/api/v1/services/s1/token token new 2026-11-01T11:00:00Z"}, seen, "no pull while a token is unsent")

	mu.Lock()
	failing, seen = false, nil
	mu.Unlock()
	l.Token("gone", "x", exp)
	snap, err = l.Tick(ctx, c, `"1"`)
	require.NoError(t, err)
	require.Equal(t, `"2"`, snap.Rev)
	require.False(t, l.Pending(), "sent and gone tokens both leave the queue")
	require.Len(t, seen, 3)
	require.ElementsMatch(t, []string{
		"/api/v1/services/s1/token token new 2026-11-01T11:00:00Z",
		"/api/v1/services/gone/token token x 2026-11-01T10:00:00Z",
	}, seen[:2])
	require.Equal(t, `pull "1"`, seen[2])
	require.Contains(t, logged.String(), "dropped a token write-back")
	require.Contains(t, logged.String(), "service=gone")

	l.Token("s1", "newer", exp)
	l.Reset()
	require.False(t, l.Pending())
}
