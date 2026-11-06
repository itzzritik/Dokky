package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/itzzritik/dokky/cli/internal/platform"
)

type Handler func(ctx context.Context, params json.RawMessage) (any, error)

const (
	subscriberBuffer = 64
	acceptBackoff    = 50 * time.Millisecond
)

type Server struct {
	ln       net.Listener
	handlers map[string]Handler

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	subs  map[chan Event]struct{}
}

func NewServer(ln net.Listener, handlers map[string]Handler) *Server {
	return &Server{ln: ln, handlers: handlers, conns: map[net.Conn]struct{}{}, subs: map[chan Event]struct{}{}}
}

// Watching reports whether any client, such as the TUI, is listening for events.
func (s *Server) Watching() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

// Publish drops the event for any subscriber whose buffer is full.
func (s *Server) Publish(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Server) closeAll() {
	s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
}

func transient(err error) bool {
	var ne net.Error
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ECONNABORTED) || (errors.As(err, &ne) && ne.Timeout())
}

func (s *Server) Serve(ctx context.Context) error {
	var wg sync.WaitGroup
	stop := context.AfterFunc(ctx, s.closeAll)
	defer stop()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() == nil && transient(err) {
				time.Sleep(acceptBackoff)
				continue
			}
			s.closeAll()
			wg.Wait()
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if platform.VerifyPeer(conn) != nil {
			conn.Close()
			continue
		}
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lines := make(chan []byte)
	go func() {
		defer close(lines)
		defer cancel()
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64<<10), maxMessage)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	enc := json.NewEncoder(conn)
	for {
		var line []byte
		select {
		case l, ok := <-lines:
			if !ok {
				return
			}
			line = l
		case <-ctx.Done():
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(Response{Error: &Error{Code: "bad_request", Message: "malformed request"}})
			continue
		}
		// lets a newer CLI restart an older daemon
		if req.V != Protocol && req.Method != "daemon.restart" {
			_ = enc.Encode(Response{ID: req.ID, Error: &Error{Code: "protocol_mismatch", Message: "unsupported protocol version"}})
			continue
		}
		if req.Method == "events" {
			s.stream(ctx, enc, req.ID)
			return
		}
		h, ok := s.handlers[req.Method]
		if !ok {
			_ = enc.Encode(Response{ID: req.ID, Error: &Error{Code: "unknown_method", Message: "unknown method " + req.Method}})
			continue
		}
		if err := enc.Encode(run(ctx, h, req)); err != nil {
			return
		}
	}
}

func run(ctx context.Context, h Handler, req Request) (resp Response) {
	defer func() {
		if r := recover(); r != nil {
			resp = Response{ID: req.ID, Error: &Error{Code: "internal", Message: "handler panic"}}
		}
	}()
	out, err := h(ctx, req.Params)
	if err != nil {
		code := "internal"
		var ce CodedError
		var pe *Error
		if errors.As(err, &ce) {
			code = ce.Code()
		} else if errors.As(err, &pe) {
			code = pe.Code
		}
		return Response{ID: req.ID, Error: &Error{Code: code, Message: err.Error()}}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return Response{ID: req.ID, Error: &Error{Code: "internal", Message: err.Error()}}
	}
	return Response{ID: req.ID, Result: raw}
}

func (s *Server) stream(ctx context.Context, enc *json.Encoder, id uint64) {
	ch := make(chan Event, subscriberBuffer)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	if enc.Encode(Response{ID: id}) != nil {
		return
	}
	for {
		select {
		case ev := <-ch:
			if enc.Encode(ev) != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
