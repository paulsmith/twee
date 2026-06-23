package main

import (
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("trace", runTrace)

	registerUsage("trace", `twee trace start [--out <path.twee>] [--name <name>]
twee trace stop [--name <name>]
Start/stop a trace recording on the running session.

Trace bundles are .twee zip files containing:
  manifest.json          session metadata: command, size, pid, host, times
  events.jsonl           timestamped PTY output, input, resize, and exit events

If --out is omitted, trace start writes to a temporary path and prints it in the
JSON response. Trace stop finalizes the bundle and prints the saved path.

A trace left active when the child exits is finalized automatically: the
daemon writes the bundle before tearing down, and "twee wait exit" blocks
until it is durable and reports it as {"trace_path": ...}. To record an
entire session from spawn to teardown, use "twee start --trace <path.twee>".`)
	registerUsage("trace start", `twee trace start [--out <path.twee>] [--name <name>]
Start a trace recording on the running session.

While tracing is active, twee records PTY output bytes, input events, resize
events, and the process exit. If --out is omitted, twee creates a temporary
.twee path and returns it as {"out": "..."}.

Example:
  twee trace start --out /tmp/session.twee
  twee key Enter
  twee trace stop`)
	registerUsage("trace stop", `twee trace stop [--name <name>]
Stop a trace recording and write the .twee bundle.

Trace stop closes the trace and returns the saved path as {"path": "..."}.`)
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
		callSessionAndEmit("trace start", opts.Name, rpc.OpTraceStart, rpc.TraceStartArgs{Out: opts.Out})
	case "stop":
		var opts struct {
			Name *string `arg:"--name"`
		}
		if err := parseArg("trace stop", &opts, rest); err != nil {
			fatalUsage("trace stop: %v", err)
		}
		name := mustResolveSessionNamePtr("trace stop", opts.Name)
		resp, err := callDaemon(name, rpc.OpTraceStop, nil)
		if err != nil {
			msg := err.Error()
			code := transportErrorCode(err)
			if code == rpc.CodeNotFound {
				msg += "; if the child already exited, an active trace was finalized automatically to its --out path (see 'twee help trace')"
			}
			emitError(code, msg, nil, 1)
		}
		if !resp.OK {
			emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
		}
		emitOKRaw(resp.Data)
	default:
		fatalUsage("trace: unknown subverb %q", sub)
	}
}
