package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/itzzritik/dokky/cli/internal/platform"
	"github.com/stretchr/testify/require"
)

type codedErr struct{}

func (codedErr) Error() string { return "nope" }
func (codedErr) Code() string  { return "nope_code" }

func sockDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "ipc")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func start(t *testing.T, handlers map[string]Handler) (*Server, string) {
	t.Helper()
	dir := sockDir(t)
	ln, err := platform.Listen(platform.SocketAddr(dir))
	require.NoError(t, err)
	srv := NewServer(ln, handlers)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return")
		}
	})
	return srv, dir
}

func echo(_ context.Context, p json.RawMessage) (any, error) {
	var v map[string]any
	if err := json.Unmarshal(p, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func TestCallRoundTrip(t *testing.T) {
	_, dir := start(t, map[string]Handler{"echo": echo})
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	var out map[string]any
	require.NoError(t, c.Call(context.Background(), "echo", map[string]any{"a": "b"}, &out))
	require.Equal(t, "b", out["a"])
}

func TestUnknownMethod(t *testing.T) {
	_, dir := start(t, nil)
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	err = c.Call(context.Background(), "nope", nil, nil)
	var e *Error
	require.True(t, errors.As(err, &e))
	require.Equal(t, "unknown_method", e.Code)
}

func TestProtocolMismatch(t *testing.T) {
	_, dir := start(t, map[string]Handler{"echo": echo})
	conn, err := platform.Dial(platform.SocketAddr(dir), time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(`{"v":99,"id":7,"method":"echo"}` + "\n"))
	require.NoError(t, err)
	var resp Response
	require.NoError(t, json.NewDecoder(conn).Decode(&resp))
	require.Equal(t, uint64(7), resp.ID)
	require.Equal(t, "protocol_mismatch", resp.Error.Code)
}

func TestEventsStream(t *testing.T) {
	srv, dir := start(t, nil)
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Events(ctx)
	require.NoError(t, err)
	srv.Publish(Event{Event: "status", Data: json.RawMessage(`{"n":1}`)})
	srv.Publish(Event{Event: "transfers", Data: json.RawMessage(`{"n":2}`)})
	for _, want := range []string{"status", "transfers"} {
		select {
		case ev := <-ch:
			require.Equal(t, want, ev.Event)
		case <-time.After(2 * time.Second):
			t.Fatal("event timeout")
		}
	}
}

func TestHandlerErrorCodes(t *testing.T) {
	_, dir := start(t, map[string]Handler{
		"coded": func(context.Context, json.RawMessage) (any, error) { return nil, codedErr{} },
		"plain": func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("boom") },
	})
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	var e *Error
	require.True(t, errors.As(c.Call(context.Background(), "coded", nil, nil), &e))
	require.Equal(t, "nope_code", e.Code)
	require.True(t, errors.As(c.Call(context.Background(), "plain", nil, nil), &e))
	require.Equal(t, "internal", e.Code)
	require.Equal(t, "boom", e.Message)
}

func TestLargeReply(t *testing.T) {
	big := strings.Repeat("x", 3<<20)
	_, dir := start(t, map[string]Handler{
		"big": func(context.Context, json.RawMessage) (any, error) { return big, nil },
	})
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	var out string
	require.NoError(t, c.Call(context.Background(), "big", nil, &out))
	require.Len(t, out, len(big))
}

func TestConcurrentCalls(t *testing.T) {
	_, dir := start(t, map[string]Handler{"echo": echo})
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	errs := make(chan error, 20)
	for i := range 20 {
		go func() {
			var out map[string]any
			err := c.Call(context.Background(), "echo", map[string]any{"i": i}, &out)
			if err == nil && out["i"] != float64(i) {
				err = errors.New("mismatched reply")
			}
			errs <- err
		}()
	}
	for range 20 {
		require.NoError(t, <-errs)
	}
}

func TestHandlerCtxCancelledOnDisconnect(t *testing.T) {
	cancelled := make(chan struct{})
	started := make(chan struct{})
	_, dir := start(t, map[string]Handler{
		"hang": func(ctx context.Context, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		},
	})
	conn, err := platform.Dial(platform.SocketAddr(dir), time.Second)
	require.NoError(t, err)
	_, err = conn.Write([]byte(`{"v":1,"id":1,"method":"hang"}` + "\n"))
	require.NoError(t, err)
	<-started
	conn.Close()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler ctx not cancelled")
	}
}

func TestServeClosesConnections(t *testing.T) {
	dir := sockDir(t)
	ln, err := platform.Listen(platform.SocketAddr(dir))
	require.NoError(t, err)
	srv := NewServer(ln, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	evCtx, evCancel := context.WithCancel(context.Background())
	defer evCancel()
	ch, err := c.Events(evCtx)
	require.NoError(t, err)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	select {
	case _, ok := <-ch:
		require.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("event stream not closed")
	}
	_, err = platform.Dial(platform.SocketAddr(dir), 200*time.Millisecond)
	require.Error(t, err)
}

func TestPublishNeverBlocks(t *testing.T) {
	srv, dir := start(t, nil)
	conn, err := platform.Dial(platform.SocketAddr(dir), time.Second)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte(`{"v":1,"id":1,"method":"events"}` + "\n"))
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		for range 100000 {
			srv.Publish(Event{Event: "status", Data: json.RawMessage(`{"pad":"` + strings.Repeat("y", 1024) + `"}`)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
}

type failingListener struct {
	net.Listener
	n, failAt int
	gate      chan struct{}
}

func (f *failingListener) Accept() (net.Conn, error) {
	f.n++
	if f.n >= f.failAt {
		<-f.gate
		return nil, errors.New("accept failed")
	}
	return f.Listener.Accept()
}

func TestServeReturnsOnAcceptError(t *testing.T) {
	dir := sockDir(t)
	ln, err := platform.Listen(platform.SocketAddr(dir))
	require.NoError(t, err)
	gate := make(chan struct{})
	srv := NewServer(&failingListener{Listener: ln, failAt: 3, gate: gate}, nil)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background()) }()
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	ch, err := c.Events(context.Background())
	require.NoError(t, err)
	close(gate)
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Serve hung after accept error")
	}
	select {
	case _, ok := <-ch:
		require.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("event stream not closed")
	}
}

func TestHandlerPanicBecomesInternal(t *testing.T) {
	_, dir := start(t, map[string]Handler{
		"panic": func(context.Context, json.RawMessage) (any, error) { panic("x") },
		"echo":  echo,
	})
	c, err := Connect(dir)
	require.NoError(t, err)
	defer c.Close()
	var e *Error
	require.True(t, errors.As(c.Call(context.Background(), "panic", nil, nil), &e))
	require.Equal(t, "internal", e.Code)
	var out map[string]any
	require.NoError(t, c.Call(context.Background(), "echo", map[string]any{"a": 1}, &out))
}
