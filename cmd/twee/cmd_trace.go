package main

import (
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("trace", runTrace)

	registerUsage("trace", `twee trace start [--out <path.twee>] [--name <name>]
twee trace stop [--name <name>]
Start/stop a trace recording on the running session.

Trace bundles are .twee zip files containing:
  manifest.json          session metadata: command, size, pid, host, times
  events.jsonl           timestamped PTY output, input, and resize events
  screenshots/*.png      initial and final viewport screenshots

If --out is omitted, trace start writes to a temporary path and prints it in the
JSON response. Trace stop finalizes the bundle and prints the saved path.`)
	registerUsage("trace start", `twee trace start [--out <path.twee>] [--name <name>]
Start a trace recording on the running session.

While tracing is active, twee records PTY output bytes, input events, resize
events, and an initial screenshot. If --out is omitted, twee creates a temporary
.twee path and returns it as {"out": "..."}.

Example:
  twee trace start --out /tmp/session.twee
  twee key Enter
  twee trace stop`)
	registerUsage("trace stop", `twee trace stop [--name <name>]
Stop a trace recording and write the .twee bundle.

Trace stop captures a final screenshot, closes the trace, and returns the saved
path as {"path": "..."}.`)
}

func runTrace(args []string) {
	if len(args) == 0 {
		fatalUsage("trace: missing subverb (start|stop)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "start":
		var opts struct {
			Name *string `arg:"--name"`
			Out  string  `arg:"--out"`
		}
		if err := parseArg("trace start", &opts, rest); err != nil {
			fatalUsage("trace start: %v", err)
		}
		callAndEmit(mustCurrentSessionName("trace start", nameOptFromPtr(opts.Name)), rpc.OpTraceStart, rpc.TraceStartArgs{Out: opts.Out})
	case "stop":
		var opts struct {
			Name *string `arg:"--name"`
		}
		if err := parseArg("trace stop", &opts, rest); err != nil {
			fatalUsage("trace stop: %v", err)
		}
		callAndEmit(mustCurrentSessionName("trace stop", nameOptFromPtr(opts.Name)), rpc.OpTraceStop, nil)
	default:
		fatalUsage("trace: unknown subverb %q", sub)
	}
}
