package main

import (
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("status", runStatus)
	registerUsage("status", `twee status [--name <name>]
Returns {name, socket, pid, cmd, size, started_at, running, exit_code}.
"running" is true while the child is alive; once it exits, "running"
becomes false and "exit_code" is populated.`)
}

func runStatus(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("status", &opts, args); err != nil {
		fatalUsage("status: %v", err)
	}
	resp, err := callDaemon(mustCurrentSessionName("status", nameOptFromPtr(opts.Name)), rpc.OpStatus, nil)
	if err != nil {
		emitError(rpc.CodeNotFound, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}
