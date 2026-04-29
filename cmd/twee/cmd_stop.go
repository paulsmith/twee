package main

import (
	"flag"
	"os"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("stop", runStop)
	registerUsage("stop", `twee stop [-name <name>]
SIGTERM the child, wait 250ms, escalate to SIGKILL, then remove the
daemon's socket and lock file. Returns {"name": ..., "stopped": true}.`)
}

func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("stop: %v", err)
	}
	resp, err := callDaemon(*name, rpc.OpStop, nil)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	if sp, err := socketPath(*name); err == nil {
		_ = os.Remove(sp)
	}
	if lp, err := lockPath(*name); err == nil {
		_ = os.Remove(lp)
	}
	emitOK(map[string]any{"name": *name, "stopped": true})
}
