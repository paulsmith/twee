package main

import (
	"flag"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("record", runRecord)

	registerUsage("record", `twee record start [-out <path.jsonl>] [-name <name>]
twee record stop [-name <name>]
Toggle JSONL recording on the running session.`)
	registerUsage("record start", `twee record start [-out <path.jsonl>] [-name <name>]
Start JSONL recording on the running session.`)
	registerUsage("record stop", `twee record stop [-name <name>]
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
		fs := flag.NewFlagSet("record start", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		out := fs.String("out", "", "output path")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("record start: %v", err)
		}
		callAndEmit(*name, rpc.OpRecordStart, rpc.RecordStartArgs{Out: *out})
	case "stop":
		fs := flag.NewFlagSet("record stop", flag.ExitOnError)
		name := fs.String("name", "default", "session name")
		if err := fs.Parse(rest); err != nil {
			fatalUsage("record stop: %v", err)
		}
		callAndEmit(*name, rpc.OpRecordStop, nil)
	default:
		fatalUsage("record: unknown subverb %q", sub)
	}
}
