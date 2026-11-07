package nfs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	xdr2 "github.com/rasky/go-xdr/xdr2"
	"github.com/willscott/go-nfs-client/nfs/rpc"
	"github.com/willscott/go-nfs-client/nfs/xdr"
)

var (
	// ErrInputInvalid is returned when input cannot be parsed
	ErrInputInvalid = errors.New("invalid input")
	// ErrAlreadySent is returned when writing a header/status multiple times
	ErrAlreadySent = errors.New("response already started")
)

// ResponseCode is a combination of accept_stat and reject_stat.
type ResponseCode uint32

// ResponseCode Codes
const (
	ResponseCodeSuccess ResponseCode = iota
	ResponseCodeProgUnavailable
	ResponseCodeProcUnavailable
	ResponseCodeGarbageArgs
	ResponseCodeSystemErr
	ResponseCodeRPCMismatch
	ResponseCodeAuthError
)

type conn struct {
	*Server
	writeSerializer chan []byte
	net.Conn
}

func (c *conn) serve(ctx context.Context) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.writeSerializer = make(chan []byte, 1)
	go c.serializeWrites(connCtx)

	bio := bufio.NewReader(c.Conn)
	for {
		w, err := c.readRequestHeader(connCtx, bio)
		if err != nil {
			if err == io.EOF {
				// Clean close.
				c.Close()
				return
			}
			return
		}
		Log.Tracef("request: %v", w.req)
		// dokky: requests run side by side, so one slow call never holds up the whole volume
		go c.answer(connCtx, w)
	}
}

// dokky: macOS calls a server not responding after 5s, so slower calls are told to try later (muted by mutejukebox)
const busyAfter = 4 * time.Second

func (c *conn) answer(ctx context.Context, w *response) {
	key := flightKey{c.Server, w.req.sum()}
	f, fresh := joinFlight(key)
	if fresh {
		// dokky: the call outlives this connection, so a retry on a new one still gets its answer
		run := context.WithoutCancel(ctx)
		go func() {
			unlock := lockFile(w.req.file, w.req.write)
			err := c.handle(run, w)
			unlock()
			f.land(w.writer.Bytes(), err)
		}()
	}
	t := time.NewTimer(busyAfter)
	defer t.Stop()
	select {
	case <-f.done:
	case <-t.C:
		body, ok := busyBody(w.req)
		if !ok {
			<-f.done
			break
		}
		busy := &response{conn: c, req: w.req, writer: bytes.NewBuffer([]byte{}), errorFmt: errFormatterWithBody(body)}
		if err := c.err(ctx, busy, &NFSStatusError{NFSStatusJukebox, nil}); err != nil || busy.finish(ctx) != nil {
			c.Close()
		}
		return
	}
	dropFlight(key, f)
	if f.err != nil {
		Log.Errorf("error handling req: %v", f.err)
		// failure to handle at a level needing to close the connection.
		c.Close()
		return
	}
	reply := append([]byte(nil), f.reply...)
	if len(reply) >= 4 {
		binary.BigEndian.PutUint32(reply, w.req.xid)
	}
	select {
	case c.writeSerializer <- reply:
	case <-ctx.Done():
		Log.Errorf("error sending response: %v", ctx.Err())
		c.Close()
	}
}

// dokky: a call still running when its busy reply went out keeps going; macOS retries the same call
// and gets that answer, instead of starting it again and waiting forever on a slow or failing cloud
const flightKeep = time.Minute

type flightKey struct {
	srv *Server
	sum [sha256.Size]byte
}

type flight struct {
	done   chan struct{}
	reply  []byte
	err    error
	landed time.Time
}

var flights = struct {
	sync.Mutex
	m map[flightKey]*flight
}{m: map[flightKey]*flight{}}

func joinFlight(k flightKey) (*flight, bool) {
	flights.Lock()
	defer flights.Unlock()
	now := time.Now()
	for key, f := range flights.m {
		if !f.landed.IsZero() && now.Sub(f.landed) > flightKeep {
			delete(flights.m, key)
		}
	}
	if f, ok := flights.m[k]; ok {
		return f, false
	}
	f := &flight{done: make(chan struct{})}
	flights.m[k] = f
	return f, true
}

func (f *flight) land(reply []byte, err error) {
	flights.Lock()
	f.reply, f.err, f.landed = reply, err, time.Now()
	flights.Unlock()
	close(f.done)
}

func dropFlight(k flightKey, f *flight) {
	flights.Lock()
	if flights.m[k] == f {
		delete(flights.m, k)
	}
	flights.Unlock()
}

// dokky: the zeroed failure data each NFSv3 reply carries after its status (RFC 1813)
func busyBody(r *request) ([]byte, bool) {
	if r.Header.Prog != nfsServiceID || r.Header.Vers != 3 {
		return nil, false
	}
	switch NFSProcedure(r.Header.Proc) {
	case NFSProcedureNull:
		return nil, false
	case NFSProcedureGetAttr:
		return nil, true
	case NFSProcedureSetAttr, NFSProcedureWrite, NFSProcedureCreate, NFSProcedureMkDir, NFSProcedureSymlink,
		NFSProcedureMkNod, NFSProcedureRemove, NFSProcedureRmDir, NFSProcedureCommit:
		return make([]byte, 8), true
	case NFSProcedureLink:
		return make([]byte, 12), true
	case NFSProcedureRename:
		return make([]byte, 16), true
	}
	return make([]byte, 4), true
}

func (c *conn) serializeWrites(ctx context.Context) {
	// dokky: a reply that can't be written closes the connection, so macOS reconnects instead of waiting forever
	defer c.Close()
	// todo: maybe don't need the extra buffer
	writer := bufio.NewWriter(c.Conn)
	var fragmentBuf [4]byte
	var fragmentInt uint32
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.writeSerializer:
			if !ok {
				return
			}
			// prepend the fragmentation header
			fragmentInt = uint32(len(msg))
			fragmentInt |= (1 << 31)
			binary.BigEndian.PutUint32(fragmentBuf[:], fragmentInt)
			n, err := writer.Write(fragmentBuf[:])
			if n < 4 || err != nil {
				return
			}
			n, err = writer.Write(msg)
			if err != nil {
				return
			}
			if n < len(msg) {
				panic("todo: ensure writes complete fully.")
			}
			if err = writer.Flush(); err != nil {
				return
			}
		}
	}
}

// Handle a request. errors from this method indicate a failure to read or
// write on the network stream, and trigger a disconnection of the connection.
func (c *conn) handle(ctx context.Context, w *response) error {
	handler := c.Server.handlerFor(w.req.Header.Prog, w.req.Header.Proc)
	if handler == nil {
		Log.Errorf("No handler for %d.%d", w.req.Header.Prog, w.req.Header.Proc)
		if err := w.drain(ctx); err != nil {
			return err
		}
		return c.err(ctx, w, &ResponseCodeProcUnavailableError{})
	}
	appError := handler(ctx, w, c.Server.Handler)
	if drainErr := w.drain(ctx); drainErr != nil {
		return drainErr
	}
	if appError != nil && !w.responded {
		if err := c.err(ctx, w, appError); err != nil {
			return err
		}
	}
	if !w.responded {
		Log.Errorf("Handler did not indicate response status via writing or erroring")
		if err := c.err(ctx, w, &ResponseCodeSystemError{}); err != nil {
			return err
		}
	}
	return nil
}

func (c *conn) err(ctx context.Context, w *response, err error) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	if w.err == nil {
		w.err = err
	}

	if w.responded {
		return nil
	}

	rpcErr := w.errorFmt(err)
	if writeErr := w.writeHeader(rpcErr.Code()); writeErr != nil {
		return writeErr
	}

	body, _ := rpcErr.MarshalBinary()
	return w.Write(body)
}

type request struct {
	xid uint32
	rpc.Header
	Body  io.Reader
	raw   []byte
	file  string
	write bool
}

// dokky: the same call sent again has the same program, procedure and arguments
func (r *request) sum() [sha256.Size]byte {
	h := sha256.New()
	_ = binary.Write(h, binary.BigEndian, [3]uint32{r.Header.Prog, r.Header.Vers, r.Header.Proc})
	h.Write(r.raw)
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

// dokky: calls on one file take turns, since rclone's cache drops a write that extends a file while another call opens it
var fileLocks = struct {
	sync.Mutex
	m map[string]*fileLock
}{m: map[string]*fileLock{}}

type fileLock struct {
	sync.RWMutex
	refs int
}

func lockFile(key string, write bool) func() {
	if key == "" {
		return func() {}
	}
	fileLocks.Lock()
	l := fileLocks.m[key]
	if l == nil {
		l = &fileLock{}
		fileLocks.m[key] = l
	}
	l.refs++
	fileLocks.Unlock()
	if write {
		l.Lock()
	} else {
		l.RLock()
	}
	return func() {
		if write {
			l.Unlock()
		} else {
			l.RUnlock()
		}
		fileLocks.Lock()
		if l.refs--; l.refs == 0 {
			delete(fileLocks.m, key)
		}
		fileLocks.Unlock()
	}
}

// dokky: every NFSv3 call here starts with the file handle (RFC 1813)
func fileOf(h rpc.Header, body []byte) (file string, write bool) {
	if h.Prog != nfsServiceID || h.Vers != 3 || len(body) < 4 {
		return "", false
	}
	switch NFSProcedure(h.Proc) {
	case NFSProcedureRead:
	case NFSProcedureWrite, NFSProcedureCommit, NFSProcedureSetAttr:
		write = true
	default:
		return "", false
	}
	n := binary.BigEndian.Uint32(body)
	if n == 0 || n > 64 || 4+int(n) > len(body) {
		return "", false
	}
	return string(body[4 : 4+n]), write
}

func (r *request) String() string {
	if r.Header.Prog == nfsServiceID {
		return fmt.Sprintf("RPC #%d (nfs.%s)", r.xid, NFSProcedure(r.Header.Proc))
	} else if r.Header.Prog == mountServiceID {
		return fmt.Sprintf("RPC #%d (mount.%s)", r.xid, MountProcedure(r.Header.Proc))
	}
	return fmt.Sprintf("RPC #%d (%d.%d)", r.xid, r.Header.Prog, r.Header.Proc)
}

type response struct {
	*conn
	writer    *bytes.Buffer
	responded bool
	err       error
	errorFmt  func(error) RPCError
	req       *request
}

func (w *response) writeXdrHeader() error {
	err := xdr.Write(w.writer, &w.req.xid)
	if err != nil {
		return err
	}
	respType := uint32(1)
	err = xdr.Write(w.writer, &respType)
	if err != nil {
		return err
	}
	return nil
}

func (w *response) writeHeader(code ResponseCode) error {
	if w.responded {
		return ErrAlreadySent
	}
	w.responded = true
	if err := w.writeXdrHeader(); err != nil {
		return err
	}

	status := rpc.MsgAccepted
	if code == ResponseCodeAuthError || code == ResponseCodeRPCMismatch {
		status = rpc.MsgDenied
	}

	err := xdr.Write(w.writer, &status)
	if err != nil {
		return err
	}

	if status == rpc.MsgAccepted {
		// Write opaque_auth header.
		err = xdr.Write(w.writer, &rpc.AuthNull)
		if err != nil {
			return err
		}
	}

	return xdr.Write(w.writer, &code)
}

// Write a response to an xdr message
func (w *response) Write(dat []byte) error {
	if !w.responded {
		if err := w.writeHeader(ResponseCodeSuccess); err != nil {
			return err
		}
	}

	acc := 0
	for acc < len(dat) {
		n, err := w.writer.Write(dat[acc:])
		if err != nil {
			return err
		}
		acc += n
	}
	return nil
}

// drain reads the rest of the request frame if not consumed by the handler.
func (w *response) drain(ctx context.Context) error {
	if reader, ok := w.req.Body.(*io.LimitedReader); ok {
		if reader.N == 0 {
			return nil
		}
		// todo: wrap body in a context reader.
		_, err := io.CopyN(io.Discard, w.req.Body, reader.N)
		if err == nil || err == io.EOF {
			return nil
		}
		return err
	}
	return io.ErrUnexpectedEOF
}

func (w *response) finish(ctx context.Context) error {
	select {
	case w.conn.writeSerializer <- w.writer.Bytes():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *conn) readRequestHeader(ctx context.Context, reader *bufio.Reader) (w *response, err error) {
	fragment, err := xdr.ReadUint32(reader)
	if err != nil {
		if xdrErr, ok := err.(*xdr2.UnmarshalError); ok {
			if xdrErr.Err == io.EOF {
				return nil, io.EOF
			}
		}
		return nil, err
	}
	if fragment&(1<<31) == 0 {
		Log.Warnf("Warning: haven't implemented fragment reconstruction.\n")
		return nil, ErrInputInvalid
	}
	reqLen := fragment - uint32(1<<31)
	if reqLen < 40 {
		return nil, ErrInputInvalid
	}

	r := io.LimitedReader{R: reader, N: int64(reqLen)}

	xid, err := xdr.ReadUint32(&r)
	if err != nil {
		return nil, err
	}
	reqType, err := xdr.ReadUint32(&r)
	if err != nil {
		return nil, err
	}
	if reqType != 0 { // 0 = request, 1 = response
		return nil, ErrInputInvalid
	}

	req := request{
		xid:    xid,
		Header: rpc.Header{},
		Body:   &r,
	}
	if err = xdr.Read(&r, &req.Header); err != nil {
		return nil, err
	}
	// dokky: the body is read now, so the next request can be read while this one runs
	body, err := io.ReadAll(&r)
	if err != nil {
		return nil, err
	}
	req.Body = &io.LimitedReader{R: bytes.NewReader(body), N: int64(len(body))}
	req.raw = body
	req.file, req.write = fileOf(req.Header, body)

	w = &response{
		conn:     c,
		req:      &req,
		errorFmt: basicErrorFormatter,
		// TODO: use a pool for these.
		writer: bytes.NewBuffer([]byte{}),
	}
	return w, nil
}
