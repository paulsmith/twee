package main

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

// callDaemon dials the named session's socket and runs one op.
func callDaemon(name, op string, args any) (rpc.Response, error) {
	sock, err := socketPath(name)
	if err != nil {
		return rpc.Response{}, err
	}
	c, err := dialUnixSocketTimeout(sock, 2*time.Second)
	if err != nil {
		return rpc.Response{}, fmt.Errorf("dial %s: %w", sock, err)
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

// callAndEmit calls one op and prints the JSON envelope, exiting on error.
func callAndEmit(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}

// callOnly calls one op, exits on error, but stays silent on success.
func callOnly(name, op string, args any) {
	resp, err := callDaemon(name, op, args)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
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
