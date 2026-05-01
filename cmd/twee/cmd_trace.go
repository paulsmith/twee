package main

import (
	"flag"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("trace", runTrace)

	registerUsage("trace", `twee trace start [-out <path.twee>] [-name <name>]
twee trace stop [-name <name>]
Start/stop a trace recording on the running session.
The trace is a .twee zip bundle containing a manifest, events, and screenshots.`)
	registerUsage("trace start", `twee trace start [-out <path.twee>] [-name <name>]
Start a trace recording on the running session.`)
	registerUsage("trace stop", `twee trace stop [-name <name>]
Stop a trace recording and write the .twee bundle.`)
}

func runTrace(args []string) {
	if len(args) == 0 {
		fatalUsage("trace: missing subverb (start|stop)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "start":
		fs := flag.NewFlagSet("trace start", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		out := fs.String("out", "", "output path (.twee)")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("trace start: %v", err)
		}
		callAndEmit(*name, rpc.OpTraceStart, rpc.TraceStartArgs{Out: *out})
	case "stop":
		fs := flag.NewFlagSet("trace stop", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("trace stop: %v", err)
		}
		callAndEmit(*name, rpc.OpTraceStop, nil)
	default:
		fatalUsage("trace: unknown subverb %q", sub)
	}
}
