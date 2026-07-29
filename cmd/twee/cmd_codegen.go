package main

import (
	"context"
	"fmt"
	"os"

	"github.com/paulsmith/twee/internal/codegen"
)

func init() {
	register("codegen", runCodegen)
	registerUsage("codegen", `twee codegen [codegen options] -- <cmd> [args...]
Interactive script authoring mode. Starts <cmd> under a PTY, proxies
your terminal to it, and writes a replayable twee run JSON script.

Controls:
  Ctrl+] q        stop recording, terminate the child, write the script
  Ctrl+] t        toggle trace recording when no full-session trace is active
  Ctrl+] d        reserved for future detach/session support

Flags:
  --out <path>    output script path (required)
  --trace-out <path.twee>
                  record a full-session trace bundle
  --cols <int>    initial columns (default: terminal width or 80)
  --rows <int>    initial rows (default: terminal height or 24)
  --dir <path>    child working directory (default: inherit)
  --env KEY=VALUE environment override (repeatable)
  --no-waits      do not insert wait_stable sync ops

The explicit "--" boundary is required before child argv.`)
}

func runCodegen(args []string) {
	opts, err := parseCodegenArgs(args)
	if err != nil {
		fatalUsage("codegen: %v", err)
	}
	if err := codegen.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "twee codegen: %v\n", err)
		os.Exit(1)
	}
}

func parseCodegenArgs(args []string) (codegen.Options, error) {
	var opts codegen.Options
	opts.Env = map[string]string{}
	before, cmd, err := splitExplicitBoundary("codegen", args)
	if err != nil {
		return opts, err
	}
	var parsed struct {
		OutPath   string   `arg:"--out"`
		TracePath string   `arg:"--trace-out"`
		Cols      *string  `arg:"--cols"`
		Rows      *string  `arg:"--rows"`
		Dir       string   `arg:"--dir"`
		Env       []string `arg:"--env,separate"`
		NoWaits   bool     `arg:"--no-waits"`
	}
	if err := requireSeparateValues(before, "--env"); err != nil {
		return opts, err
	}
	if err := parseArg("codegen", &parsed, before); err != nil {
		return opts, err
	}
	if parsed.OutPath == "" {
		return opts, fmt.Errorf("missing --out")
	}
	if n, ok, err := positiveIntFlag("--cols", parsed.Cols); err != nil {
		return opts, err
	} else if ok {
		opts.Cols = n
	}
	if n, ok, err := positiveIntFlag("--rows", parsed.Rows); err != nil {
		return opts, err
	} else if ok {
		opts.Rows = n
	}
	for _, kv := range parsed.Env {
		k, v, ok := splitKV(kv)
		if !ok || k == "" {
			return opts, fmt.Errorf("bad --env value %q (want KEY=VALUE)", kv)
		}
		opts.Env[k] = v
	}
	opts.OutPath = parsed.OutPath
	opts.TracePath = parsed.TracePath
	opts.Dir = parsed.Dir
	opts.NoWaits = parsed.NoWaits
	opts.Command = cmd
	return opts, nil
}
