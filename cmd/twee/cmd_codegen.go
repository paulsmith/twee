package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/paulsmith/twee/internal/codegen"
)

func init() {
	register("wrap", runWrap)
	registerUsage("wrap", `twee wrap [options] -- <cmd> [args...]
Wrap <cmd> in a PTY. Script and trace recording are independently optional.

Controls:
  Ctrl+] q        finalize active recorders and terminate the child
  Ctrl+] s        start/stop JSON script capture (one-shot)
  Ctrl+] t        start/stop trace recording (one-shot; network traces run until exit)

Flags:
  --script-out <path.json>
                  start JSON script recording immediately
  --trace-out <path.twee>
                  start trace recording immediately
  --network-capture
                  capture the managed program's IPv4 traffic (Linux; requires --trace-out)
  --publish-tcp <listen=guest-port>
                  publish LISTEN_IPV4:PORT=GUEST_PORT (repeatable)
  --cols <int>    initial columns (default: terminal width or 80)
  --rows <int>    initial rows (default: terminal height or 24)
  --dir <path>    child working directory (default: inherit)
  --env KEY=VALUE environment override (repeatable)
  --no-waits      do not insert wait_stable sync ops
  --no-status     do not reserve a parent-owned status row

The explicit "--" boundary is required before child argv.`)
}

func runWrap(args []string) {
	opts, err := parseWrapArgs(args)
	if err != nil {
		fatalUsage("wrap: %v", err)
	}
	if err := codegen.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "twee wrap: %v\n", err)
		var exitErr *codegen.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}

func parseWrapArgs(args []string) (codegen.Options, error) {
	var opts codegen.Options
	before, cmd, err := splitExplicitBoundary("wrap", args)
	if err != nil {
		return opts, err
	}
	var parsed struct {
		OutPath        string   `arg:"--script-out"`
		TracePath      string   `arg:"--trace-out"`
		Cols           *string  `arg:"--cols"`
		Rows           *string  `arg:"--rows"`
		Dir            string   `arg:"--dir"`
		Env            []string `arg:"--env,separate"`
		NoWaits        bool     `arg:"--no-waits"`
		NoStatus       bool     `arg:"--no-status"`
		NetworkCapture bool     `arg:"--network-capture"`
		PublishTCP     []string `arg:"--publish-tcp,separate"`
	}
	if err := requireSeparateValues(before, "--env"); err != nil {
		return opts, err
	}
	if err := requireSeparateValues(before, "--publish-tcp"); err != nil {
		return opts, err
	}
	if err := parseArg("wrap", &parsed, before); err != nil {
		return opts, err
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
	opts.Env, err = parseEnvOverrides(parsed.Env)
	if err != nil {
		return opts, err
	}
	opts.OutPath = parsed.OutPath
	opts.TracePath = parsed.TracePath
	opts.NetworkCapture = parsed.NetworkCapture
	opts.PublishTCP, err = parseNetworkCaptureFlags(opts.NetworkCapture, parsed.PublishTCP, opts.TracePath, "--trace-out")
	if err != nil {
		return opts, err
	}
	opts.Dir = parsed.Dir
	opts.NoWaits = parsed.NoWaits
	opts.NoStatus = parsed.NoStatus
	opts.Command = cmd
	return opts, nil
}
