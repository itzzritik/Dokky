package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/itzzritik/dokky/cli/internal/platform"
)

const dialTimeout = 3 * time.Second

type Client struct {
	addr string

	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Scanner
	next uint64
}

func Connect(stateDir string) (*Client, error) {
	c := &Client{addr: platform.SocketAddr(stateDir)}
	if err := c.redial(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) redial() error {
	conn, err := platform.Dial(c.addr, dialTimeout)
	if err != nil {
		return err
	}
	c.conn = conn
	c.rd = newScanner(conn)
	return nil
}

func newScanner(conn net.Conn) *bufio.Scanner {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64<<10), maxMessage)
	return sc
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// A failed call drops the connection, since the stream may be out of sync; the next call redials.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		if err := c.redial(); err != nil {
			return err
		}
	}
	resp, err := c.roundTrip(ctx, method, params)
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return err
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

func (c *Client) roundTrip(ctx context.Context, method string, params any) (resp *Response, err error) {
	c.next++
	req, err := newRequest(c.next, method, params)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Now()) })
	defer func() {
		if !stop() {
			resp, err = nil, ctx.Err()
			return
		}
		_ = c.conn.SetDeadline(time.Time{})
	}()
	if _, err := c.conn.Write(req); err != nil {
		return nil, ctxOr(ctx, err)
	}
	resp, err = readResponse(c.rd)
	if err != nil {
		return nil, ctxOr(ctx, err)
	}
	if resp.ID != c.next {
		return nil, fmt.Errorf("ipc: reply id %d, want %d", resp.ID, c.next)
	}
	return resp, nil
}

func ctxOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func newRequest(id uint64, method string, params any) ([]byte, error) {
	req := Request{V: Protocol, ID: id, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		req.Params = raw
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func readResponse(sc *bufio.Scanner) (*Response, error) {
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("ipc: connection closed")
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Events(ctx context.Context) (<-chan Event, error) {
	conn, err := platform.Dial(c.addr, dialTimeout)
	if err != nil {
		return nil, err
	}
	req, err := newRequest(1, "events", nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	sc := newScanner(conn)
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := readResponse(sc)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.Error != nil {
		conn.Close()
		return nil, resp.Error
	}
	_ = conn.SetDeadline(time.Time{})

	out := make(chan Event, subscriberBuffer)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	go func() {
		defer close(out)
		defer conn.Close()
		defer stop()
		for sc.Scan() {
			var ev Event
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
