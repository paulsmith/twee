package main

import (
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("record", runRecord)

	registerUsage("record", `twee record start [--out <path.jsonl>] [--name <name>]
twee record stop [--name <name>]
Toggle JSONL recording on the running session.`)
	registerUsage("record start", `twee record start [--out <path.jsonl>] [--name <name>]
Start JSONL recording on the running session.`)
	registerUsage("record stop", `twee record stop [--name <name>]
Stop JSONL recording on the running session.`)
}

func runRecord(args []string) {
	if len(args) == 0 {
		fatalUsage("record: missing subverb (start|stop)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "start":
		var opts struct {
			Name *string `arg:"--name"`
			Out  string  `arg:"--out"`
		}
		if err := parseArg("record start", &opts, rest); err != nil {
			fatalUsage("record start: %v", err)
		}
		callAndEmit(mustCurrentSessionName("record start", nameOptFromPtr(opts.Name)), rpc.OpRecordStart, rpc.RecordStartArgs{Out: opts.Out})
	case "stop":
		var opts struct {
			Name *string `arg:"--name"`
		}
		if err := parseArg("record stop", &opts, rest); err != nil {
			fatalUsage("record stop: %v", err)
		}
		callAndEmit(mustCurrentSessionName("record stop", nameOptFromPtr(opts.Name)), rpc.OpRecordStop, nil)
	default:
		fatalUsage("record: unknown subverb %q", sub)
	}
}
