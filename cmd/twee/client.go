package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

// dialError marks a failure to reach the session's socket at all, as
// opposed to an I/O error on an established connection. Clients map it
// to NOT_FOUND: the session is gone (or never existed).
type dialError struct {
	sock string
	err  error
}

func (e *dialError) Error() string { return fmt.Sprintf("dial %s: %v", e.sock, e.err) }
func (e *dialError) Unwrap() error { return e.err }

// dialSession dials the named session's socket, wrapping an unreachable
// or refused connection as a *dialError so callers can map it to
// NOT_FOUND via transportErrorCode. Shared by callDaemon (one op per
// call) and "do"'s script executor (one op per dial, same socket,
// repeated for each op in the script).
func dialSession(name string) (net.Conn, error) {
	sock, err := socketPath(name)
	if err != nil {
		return nil, err
	}
	c, err := dialUnixSocketTimeout(sock, 2*time.Second)
	if err != nil {
		return nil, &dialError{sock: sock, err: err}
	}
	return c, nil
}

// callDaemon dials the named session's socket and runs one op.
func callDaemon(name, op string, args any) (rpc.Response, error) {
	c, err := dialSession(name)
	if err != nil {
		return rpc.Response{}, err
	}
	defer c.Close()
	req := rpc.Request{ID: nextID(), Op: op}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return rpc.Response{}, fmt.Errorf("marshal args: %w", err)
		}
		req.Args = raw
	}
	if err := rpc.WriteMessage(c, req); err != nil {
		return rpc.Response{}, fmt.Errorf("write: %w", err)
	}
	var resp rpc.Response
	if err := rpc.ReadMessage(c, &resp); err != nil {
		return rpc.Response{}, fmt.Errorf("read: %w", err)
	}
	return resp, nil
}

var idCounter atomic.Uint64

func nextID() string {
	return fmt.Sprintf("%d", idCounter.Add(1))
}

// transportErrorCode classifies a callDaemon error: unreachable socket
// means the session is gone (NOT_FOUND); anything else is an I/O fault.
func transportErrorCode(err error) string {
	var de *dialError
	if errors.As(err, &de) {
		return rpc.CodeNotFound
	}
	return rpc.CodeIO
}

// callAndEmit calls one op and prints the JSON envelope, exiting on error.
func callAndEmit(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(transportErrorCode(err), err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}

func callSessionAndEmit(verb string, localName *string, op string, args any) {
	callAndEmit(mustResolveSessionNamePtr(verb, localName), op, args)
}

// callOnly calls one op, exits on error, but stays silent on success.
func callOnly(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(transportErrorCode(err), err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
}

// emitOKRaw writes {"ok": true, "data": <raw>} where raw is already JSON.
func emitOKRaw(data json.RawMessage) {
	if len(data) == 0 {
		emitOK(nil)
		return
	}
	out := struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}{OK: true, Data: data}
	enc := json.NewEncoder(stdoutFile())
	if err := enc.Encode(out); err != nil {
		fatalUsage("emit: %v", err)
	}
}
