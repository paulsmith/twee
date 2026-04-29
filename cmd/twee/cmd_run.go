package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

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
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	scriptPath := fs.String("script", "", "path to script JSON; '-' or empty reads stdin")
	cols := fs.Int("cols", 80, "initial cols")
	rows := fs.Int("rows", 24, "initial rows")
	dir := fs.String("dir", "", "child working dir")
	emit := fs.String("emit", "", "if 'results', stream NDJSON op responses")
	if err := fs.Parse(args); err != nil {
		fatalUsage("run: %v", err)
	}
	cmd := fs.Args()
	if len(cmd) == 0 {
		fatalUsage("run: missing command")
	}

	scriptBytes, err := readScript(*scriptPath)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	var ops []rpc.Request
	if err := json.Unmarshal(scriptBytes, &ops); err != nil {
		emitError(rpc.CodeInvalidArgument, "script: "+err.Error(), nil, 1)
	}

	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: cmd, Cols: *cols, Rows: *rows, Dir: *dir,
	})
	if err != nil {
		emitError(rpc.CodeIO, "engine.Start: "+err.Error(), nil, 1)
	}
	defer te.Close()

	tmpDir, _ := os.MkdirTemp("", "twee-run-")
	defer os.RemoveAll(tmpDir)
	sock := filepath.Join(tmpDir, "twee.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	defer l.Close()

	srv := daemon.NewServer(te)
	go srv.Serve(context.Background(), l)
	defer srv.Stop()

	emitResults := *emit == "results"
	for i, op := range ops {
		op.ID = fmt.Sprintf("%d", i)
		c, err := net.Dial("unix", sock)
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

func readScript(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
