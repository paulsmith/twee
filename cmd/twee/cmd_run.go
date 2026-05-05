package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
  -emit results    stream NDJSON op responses instead of one summary

The script is a JSON array of RPC bodies (op + args). Use the wire
op names (e.g. "wait_text", not "wait text"). See:
  docs/superpowers/specs/2026-04-28-twee-cli-design.md`)
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
	defer te.Close()

	tmpDir, _ := os.MkdirTemp("", "twee-run-")
	defer os.RemoveAll(tmpDir)
	sock := filepath.Join(tmpDir, "twee.sock")
	l, err := listenUnixSocket(sock)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	defer l.Close()

	srv := daemon.NewServer(te)
	go srv.Serve(context.Background(), l)
	defer srv.Stop()

	emitResults := opts.emit == "results"
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := dialUnixSocket(sock)
		if err != nil {
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		if err := rpc.WriteMessage(c, op); err != nil {
			c.Close()
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		var resp rpc.Response
		if err := rpc.ReadMessage(c, &resp); err != nil {
			c.Close()
			emitError(rpc.CodeIO, err.Error(), nil, 1)
		}
		c.Close()
		if emitResults {
			_ = json.NewEncoder(os.Stdout).Encode(resp)
		}
		if !resp.OK {
			if !emitResults {
				emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
			}
			os.Exit(1)
		}
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
