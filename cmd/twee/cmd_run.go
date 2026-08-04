package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/paulsmith/twee/internal/daemon"
	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("run", runRun)
	registerUsage("run", `twee run [run options] -- <cmd> [args...]
Single-shot: spin up an ephemeral daemon, execute a JSON script of
RPC ops, exit. The daemon's socket is removed on exit.

Flags:
  --script <path>  path to script JSON; "-" or empty reads stdin
  --cols <int>     initial cols (default 80)
  --rows <int>     initial rows (default 24)
  --dir <path>     child working directory
  --trace-out <path.twee>
                  record a .twee trace bundle for the whole run
  --network-capture
                  capture the managed program's IPv4 traffic (Linux; requires --trace-out)
  --publish-tcp <listen=guest>
                  publish LISTEN_IPV4:PORT=10.0.2.100:GUEST_PORT (repeatable)
  --emit results   stream NDJSON op responses instead of one summary

The script is a JSON array of RPC bodies (op + args). Use the wire
op names (e.g. "wait_text", not "wait text").

To run the same script format against an already-running named
session instead of an ephemeral one, see "twee help do".`)
}

func runRun(args []string) {
	opts, err := parseRunArgs(args)
	if err != nil {
		fatalUsage("run: %v", err)
	}
	if len(opts.cmd) == 0 {
		fatalUsage("run: missing command")
	}

	ops := loadScript(opts.scriptPath)
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

	var traceConfig *engine.WholeSessionTraceConfig
	if opts.tracePath != "" {
		traceConfig = &engine.WholeSessionTraceConfig{Path: opts.tracePath}
		if opts.networkCapture {
			traceConfig.Network = &engine.NetworkCaptureConfig{PublishTCP: opts.publishTCP}
		}
	}
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: opts.cmd, Cols: opts.cols, Rows: opts.rows, Dir: opts.dir,
		WholeSessionTrace: traceConfig,
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
		if err := te.FinalizeArtifacts(); err != nil {
			return &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
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

	traceActive = opts.tracePath != ""

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
	dial := func() (net.Conn, error) { return dialUnixSocket(sock) }
	runOpScript(ops, dial, emitResults, cleanup, fail)

	if errResp := stopTrace(); errResp != nil {
		fail(errResp.Code, errResp.Message, errResp.Details)
	}
	if !emitResults {
		emitOK(map[string]any{"ops": len(ops)})
	}
}

type runOptions struct {
	cmd            []string
	scriptPath     string
	cols           int
	rows           int
	dir            string
	emit           string
	tracePath      string
	colsSet        bool
	rowsSet        bool
	networkCapture bool
	publishTCP     []engine.TCPPublication
}

func parseRunArgs(args []string) (runOptions, error) {
	opts := runOptions{cols: 80, rows: 24}
	before, cmd, err := splitExplicitBoundary("run", args)
	if err != nil {
		return opts, err
	}
	var parsed struct {
		ScriptPath     string   `arg:"--script"`
		Cols           *string  `arg:"--cols"`
		Rows           *string  `arg:"--rows"`
		Dir            string   `arg:"--dir"`
		Emit           string   `arg:"--emit"`
		TracePath      string   `arg:"--trace-out"`
		NetworkCapture bool     `arg:"--network-capture"`
		PublishTCP     []string `arg:"--publish-tcp,separate"`
	}
	if err := parseArg("run", &parsed, before); err != nil {
		return opts, err
	}
	if err := requireSeparateValues(before, "--publish-tcp"); err != nil {
		return opts, err
	}
	if n, ok, err := positiveIntFlag("--cols", parsed.Cols); err != nil {
		return opts, err
	} else if ok {
		opts.cols = n
		opts.colsSet = true
	}
	if n, ok, err := positiveIntFlag("--rows", parsed.Rows); err != nil {
		return opts, err
	} else if ok {
		opts.rows = n
		opts.rowsSet = true
	}
	opts.scriptPath = parsed.ScriptPath
	opts.dir = parsed.Dir
	opts.emit = parsed.Emit
	tracePath, err := absOutPath(parsed.TracePath)
	if err != nil {
		return opts, err
	}
	opts.tracePath = tracePath
	opts.networkCapture = parsed.NetworkCapture
	if opts.networkCapture && tracePath == "" {
		return opts, fmt.Errorf("--network-capture requires --trace-out")
	}
	if len(parsed.PublishTCP) > 0 && !opts.networkCapture {
		return opts, fmt.Errorf("--publish-tcp requires --network-capture")
	}
	opts.publishTCP, err = parseTCPPublications(parsed.PublishTCP)
	if err != nil {
		return opts, err
	}
	opts.cmd = cmd
	return opts, nil
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
