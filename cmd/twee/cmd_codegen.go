package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/paulsmith/research/twee/internal/codegen"
)

func init() {
	register("codegen", runCodegen)
	registerUsage("codegen", `twee codegen <cmd> [args...] --out ops.json [flags]
Interactive script authoring mode. Starts <cmd> under a PTY, proxies
your terminal to it, and writes a replayable twee run JSON script.

Controls:
  Ctrl+] q        stop recording, terminate the child, write the script
  Ctrl+] d        reserved for future detach/session support

Flags:
  -out <path>     output script path (required)
  -cols <int>     initial columns (default: terminal width or 80)
  -rows <int>     initial rows (default: terminal height or 24)
  -dir <path>     child working directory (default: inherit)
  -env KEY=VALUE  environment override (repeatable)
  -no-waits       do not insert wait_stable sync ops

Flags may appear before or after the child command. Use "--" before
child args that would otherwise look like codegen flags.`)
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
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			opts.Command = append(opts.Command, args[i+1:]...)
			break
		}
		switch a {
		case "-out", "--out":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			opts.OutPath = args[i]
		case "-cols", "--cols":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("%s must be a positive integer", a)
			}
			opts.Cols = n
		case "-rows", "--rows":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("%s must be a positive integer", a)
			}
			opts.Rows = n
		case "-dir", "--dir":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			opts.Dir = args[i]
		case "-env", "--env":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			k, v, ok := splitKV(args[i])
			if !ok {
				return opts, fmt.Errorf("bad --env value %q (want KEY=VALUE)", args[i])
			}
			opts.Env[k] = v
		case "-no-waits", "--no-waits":
			opts.NoWaits = true
		default:
			opts.Command = append(opts.Command, a)
		}
	}
	if len(opts.Command) == 0 {
		return opts, fmt.Errorf("missing command")
	}
	if opts.OutPath == "" {
		return opts, fmt.Errorf("missing --out")
	}
	return opts, nil
}
