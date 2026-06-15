package main

import (
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("status", runStatus)
	registerUsage("status", `twee status [--name <name>]
Returns {cmd, cols, rows, started_at, running, exit_code}.
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
	callSessionAndEmit("status", opts.Name, rpc.OpStatus, nil)
}
