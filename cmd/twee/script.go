package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/paulsmith/twee/internal/rpc"
)

// readScript reads a script's raw JSON bytes from path; "" or "-" reads
// stdin. Shared by "run" (which drives its own ephemeral daemon) and
// "do" (which drives an existing named session) — both execute the
// identical script format.
func readScript(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// loadScript reads and decodes an op script from path, reporting and
// exiting via emitError on a read failure (CodeIO) or malformed JSON
// (CodeInvalidArgument). Shared by "run" and "do" so both fail
// identically on a bad --script.
func loadScript(path string) []rpc.Request {
	scriptBytes, err := readScript(path)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	ops, err := decodeScript(scriptBytes)
	if err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}
	clientDir, err := os.Getwd()
	if err != nil {
		emitError(rpc.CodeIO, "get client working directory: "+err.Error(), nil, 1)
	}
	if err := normalizeScriptPaths(ops, clientDir); err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}
	return ops
}

// decodeScript strictly decodes the script's request envelopes. Args remain
// raw until the operation-specific layer decodes them, but misspelled request
// fields must not disappear before then.
func decodeScript(scriptBytes []byte) ([]rpc.Request, error) {
	var ops []rpc.Request
	dec := json.NewDecoder(bytes.NewReader(scriptBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ops); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(dec); err != nil {
		return nil, err
	}
	return ops, nil
}

// normalizeScriptPaths gives op scripts the same path semantics as direct CLI
// commands: known client-supplied paths are resolved against the invoking
// client's working directory before the request crosses the RPC boundary.
// Typed decoding is deliberately strict so normalization cannot silently drop
// an unknown argument while re-marshaling the request.
func normalizeScriptPaths(ops []rpc.Request, clientDir string) error {
	for i := range ops {
		var err error
		switch ops[i].Op {
		case rpc.OpScreenshot:
			var args rpc.ScreenshotArgs
			if len(ops[i].Args) != 0 {
				if err := decodeScriptArgs(ops[i].Args, &args); err != nil {
					return fmt.Errorf("op %d %s args: %w", i, ops[i].Op, err)
				}
			}
			args.Out = resolveScriptPath(clientDir, args.Out)
			ops[i].Args, err = json.Marshal(args)
		case rpc.OpTraceStart:
			var args rpc.TraceStartArgs
			if len(ops[i].Args) != 0 {
				if err := decodeScriptArgs(ops[i].Args, &args); err != nil {
					return fmt.Errorf("op %d %s args: %w", i, ops[i].Op, err)
				}
			}
			args.Out = resolveScriptPath(clientDir, args.Out)
			ops[i].Args, err = json.Marshal(args)
		case rpc.OpDiff:
			var args rpc.DiffArgs
			if err := decodeScriptArgs(ops[i].Args, &args); err != nil {
				return fmt.Errorf("op %d %s args: %w", i, ops[i].Op, err)
			}
			args.Against = resolveScriptPath(clientDir, args.Against)
			ops[i].Args, err = json.Marshal(args)
		}
		if err != nil {
			return fmt.Errorf("op %d %s args: %w", i, ops[i].Op, err)
		}
	}
	return nil
}

func decodeScriptArgs(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return requireJSONEOF(dec)
}

func requireJSONEOF(dec *json.Decoder) error {
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected JSON after document")
		}
		return err
	}
	return nil
}

func resolveScriptPath(clientDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(clientDir, path)
}

// runOpScript executes ops in order against a daemon reached via dial,
// which must hand back a fresh connection on every call — the wire
// protocol is one request per connection (see the rpc package doc
// comment). This is the shared engine behind both "run" (which points
// dial at its own just-started ephemeral daemon) and "do" (which points
// dial at an existing named session's socket): identical script format,
// identical --emit results streaming, identical failure reporting.
//
// Without --emit results (emitResults=false): the first op that fails —
// including a transport failure just trying to reach it — is reported
// via fail, which the caller wires up to do whatever cleanup it needs
// before printing the error envelope and exiting; it never returns.
//
// With --emit results (emitResults=true): every op's response is
// streamed to stdout as one NDJSON line as soon as it completes,
// success or failure alike. So when an op fails in this mode, it has
// already been reported on stdout; runOpScript runs cleanup itself and
// exits 1 through the shared output policy instead of calling fail, which
// would print a second, redundant error envelope.
func runOpScript(ops []rpc.Request, dial func() (net.Conn, error), emitResults bool, cleanup func(), fail func(code, msg string, details json.RawMessage)) {
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := dial()
		if err != nil {
			fail(transportErrorCode(err), err.Error(), dialErrorDetails(err))
			return
		}
		if err := rpc.WriteMessage(c, op); err != nil {
			_ = c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
			return
		}
		var resp rpc.Response
		if err := rpc.ReadMessage(c, &resp); err != nil {
			_ = c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
			return
		}
		_ = c.Close()
		if emitResults {
			emitNDJSON(resp)
		}
		if !resp.OK {
			if !emitResults {
				fail(resp.Error.Code, resp.Error.Message, resp.Error.Details)
				return
			}
			exitAfterNDJSONFailure(cleanup)
		}
	}
}
