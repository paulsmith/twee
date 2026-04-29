package main

import (
	"flag"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("status", runStatus)
	registerUsage("status", `twee status [-name <name>]
Returns {name, socket, pid, cmd, size, started_at, running, exit_code}.
"running" is true while the child is alive; once it exits, "running"
becomes false and "exit_code" is populated.`)
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("status: %v", err)
	}
	resp, err := callDaemon(*name, rpc.OpStatus, nil)
	if err != nil {
		emitError(rpc.CodeNotFound, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}
