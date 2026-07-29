package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"

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
	var ops []rpc.Request
	if err := json.Unmarshal(scriptBytes, &ops); err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}
	return ops
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
// exits 1 directly instead of calling fail, which would print a second,
// redundant error envelope.
func runOpScript(ops []rpc.Request, dial func() (net.Conn, error), emitResults bool, cleanup func(), fail func(code, msg string, details json.RawMessage)) {
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := dial()
		if err != nil {
			fail(transportErrorCode(err), err.Error(), dialErrorDetails(err))
			return
		}
		if err := rpc.WriteMessage(c, op); err != nil {
			c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
			return
		}
		var resp rpc.Response
		if err := rpc.ReadMessage(c, &resp); err != nil {
			c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
			return
		}
		c.Close()
		if emitResults {
			_ = json.NewEncoder(os.Stdout).Encode(resp)
		}
		if !resp.OK {
			if !emitResults {
				fail(resp.Error.Code, resp.Error.Message, resp.Error.Details)
				return
			}
			cleanup()
			os.Exit(1)
		}
	}
}
