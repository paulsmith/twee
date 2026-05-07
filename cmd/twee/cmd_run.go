package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/paulsmith/research/twee/internal/daemon"
	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("run", runRun)
	registerUsage("run", `twee run <cmd> [args...] [flags]
Single-shot: spin up an ephemeral daemon, execute a JSON script of
RPC ops, exit. The daemon's socket is removed on exit.

Flags:
  -script <path>   path to script JSON; "-" or empty reads stdin
  -cols <int>      initial cols (default 80)
  -rows <int>      initial rows (default 24)
  -dir <path>      child working directory
  -trace-out <path.twee>
                  record a .twee trace bundle for the whole run
  -emit results    stream NDJSON op responses instead of one summary

The script is a JSON array of RPC bodies (op + args). Use the wire
op names (e.g. "wait_text", not "wait text").`)
}

func runRun(args []string) {
	opts, err := parseRunArgs(args)
	if err != nil {
		fatalUsage("run: %v", err)
	}
	if len(opts.cmd) == 0 {
		fatalUsage("run: missing command")
	}

	scriptBytes, err := readScript(opts.scriptPath)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	var ops []rpc.Request
	if err := json.Unmarshal(scriptBytes, &ops); err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}
	if !opts.colsSet || !opts.rowsSet {
		if initial, ok := leadingResize(ops); ok {
			if !opts.colsSet {
				opts.cols = initial.Cols
			}
			if !opts.rowsSet {
				opts.rows = initial.Rows
			}
		}
	}

	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: opts.cmd, Cols: opts.cols, Rows: opts.rows, Dir: opts.dir,
	})
	if err != nil {
		emitError(rpc.CodeIO, "engine.Start: "+err.Error(), nil, 1)
	}

	var (
		cleanupOnce sync.Once
		listener    net.Listener
		srv         *daemon.Server
		tmpDir      string
		traceActive bool
	)
	stopTrace := func() *rpc.Error {
		if !traceActive {
			return nil
		}
		resp, err := dispatchRunControl(te, rpc.OpTraceStop, nil)
		if err != nil {
			return &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
		}
		if !resp.OK {
			return resp.Error
		}
		traceActive = false
		return nil
	}
	cleanup := func() {
		cleanupOnce.Do(func() {
			_ = stopTrace()
			if srv != nil {
				srv.Stop()
			}
			if listener != nil {
				_ = listener.Close()
			}
			if tmpDir != "" {
				_ = os.RemoveAll(tmpDir)
			}
			_ = te.Close()
		})
	}
	defer cleanup()
	fail := func(code, msg string, details json.RawMessage) {
		cleanup()
		emitError(code, msg, details, 1)
	}

	if opts.tracePath != "" {
		resp, err := dispatchRunControl(te, rpc.OpTraceStart, rpc.TraceStartArgs{Out: opts.tracePath})
		if err != nil {
			fail(rpc.CodeInternal, err.Error(), nil)
		}
		if !resp.OK {
			fail(resp.Error.Code, resp.Error.Message, resp.Error.Details)
		}
		traceActive = true
	}

	tmpDir, err = os.MkdirTemp("", "twee-run-")
	if err != nil {
		fail(rpc.CodeIO, err.Error(), nil)
	}
	sock := filepath.Join(tmpDir, "twee.sock")
	listener, err = listenUnixSocket(sock)
	if err != nil {
		fail(rpc.CodeIO, err.Error(), nil)
	}

	srv = daemon.NewServer(te)
	go srv.Serve(context.Background(), listener)

	emitResults := opts.emit == "results"
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := dialUnixSocket(sock)
		if err != nil {
			fail(rpc.CodeIO, err.Error(), nil)
		}
		if err := rpc.WriteMessage(c, op); err != nil {
			c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
		}
		var resp rpc.Response
		if err := rpc.ReadMessage(c, &resp); err != nil {
			c.Close()
			fail(rpc.CodeIO, err.Error(), nil)
		}
		c.Close()
		if emitResults {
			_ = json.NewEncoder(os.Stdout).Encode(resp)
		}
		if !resp.OK {
			if !emitResults {
				fail(resp.Error.Code, resp.Error.Message, resp.Error.Details)
			}
			cleanup()
			os.Exit(1)
		}
	}
	if errResp := stopTrace(); errResp != nil {
		fail(errResp.Code, errResp.Message, errResp.Details)
	}
	if !emitResults {
		emitOK(map[string]any{"ops": len(ops)})
	}
}

type runOptions struct {
	cmd        []string
	scriptPath string
	cols       int
	rows       int
	dir        string
	emit       string
	tracePath  string
	colsSet    bool
	rowsSet    bool
}

func parseRunArgs(args []string) (runOptions, error) {
	opts := runOptions{cols: 80, rows: 24}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			opts.cmd = append(opts.cmd, args[i+1:]...)
			break
		}
		name, val, hasValue := splitFlagValue(arg)
		switch name {
		case "-script", "--script":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			opts.scriptPath = v
			i = next
		case "-cols", "--cols":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("%s must be a positive integer", name)
			}
			opts.cols = n
			opts.colsSet = true
			i = next
		case "-rows", "--rows":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("%s must be a positive integer", name)
			}
			opts.rows = n
			opts.rowsSet = true
			i = next
		case "-dir", "--dir":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			opts.dir = v
			i = next
		case "-emit", "--emit":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			opts.emit = v
			i = next
		case "-trace-out", "--trace-out":
			v, next, err := flagValue(name, val, hasValue, args, i)
			if err != nil {
				return opts, err
			}
			opts.tracePath = v
			i = next
		default:
			opts.cmd = append(opts.cmd, arg)
		}
	}
	return opts, nil
}

func splitFlagValue(arg string) (name, value string, ok bool) {
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

func flagValue(name, value string, hasValue bool, args []string, i int) (string, int, error) {
	if hasValue {
		return value, i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%s requires a value", name)
	}
	return args[i+1], i + 1, nil
}

func readScript(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func leadingResize(ops []rpc.Request) (rpc.ResizeArgs, bool) {
	if len(ops) == 0 || ops[0].Op != rpc.OpResize {
		return rpc.ResizeArgs{}, false
	}
	var args rpc.ResizeArgs
	if err := json.Unmarshal(ops[0].Args, &args); err != nil {
		return rpc.ResizeArgs{}, false
	}
	return args, args.Cols > 0 && args.Rows > 0
}

func dispatchRunControl(te *engine.Term, op string, args any) (rpc.Response, error) {
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return rpc.Response{}, err
		}
		raw = b
	}
	return daemon.NewDispatcher(te).Dispatch(rpc.Request{ID: op, Op: op, Args: raw}), nil
}
