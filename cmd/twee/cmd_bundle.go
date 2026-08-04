package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/paulsmith/twee/internal/bundle"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("bundle", runBundle)

	registerUsage("bundle", `twee bundle info <file.twee>
twee bundle validate <file.twee>
Inspect or verify a .twee trace bundle without opening a terminal.`)
	registerUsage("bundle info", `twee bundle info <file.twee>
Print a summary of a .twee bundle: manifest fields (version, command,
cols, rows, started_at, stopped_at) plus duration_ms, size_bytes, and
events (a count per event type, e.g. {"output":12,"input":3,...}).
The network_capture object reports optional PCAP metadata and statistics.

A missing or unreadable file fails with code IO; a file that exists but
isn't a usable bundle (bad zip, manifest, or version) fails with code
INVALID_ARGUMENT.`)
	registerUsage("bundle validate", `twee bundle validate <file.twee>
Verify a .twee bundle: zip integrity, a manifest that parses with a
supported version, every events.jsonl line parsing as a known event
type, and non-decreasing timestamps. A declared network capture is fully read
to verify its CRC, PCAP framing, raw-IP link type, limits, packet and byte
counts, truncation, and manifest/stream consistency.

On success: {"valid":true,"events":N}. On failure: exits non-zero with
code INVALID_ARGUMENT; error.details.issues lists every problem found,
not just the first. A missing or unreadable file instead fails with
code IO.`)
}

func runBundle(args []string) {
	if len(args) == 0 {
		fatalUsage("bundle: missing subverb (info|validate)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "info":
		runBundleInfo(rest)
	case "validate":
		runBundleValidate(rest)
	default:
		fatalUsage("bundle: unknown subverb %q", sub)
	}
}

func runBundleInfo(args []string) {
	path := mustBundlePath("bundle info", args)
	info, err := bundle.Inspect(path)
	if err != nil {
		emitError(bundleErrorCode(err), err.Error(), nil, 1)
	}
	emitOK(info)
}

func runBundleValidate(args []string) {
	path := mustBundlePath("bundle validate", args)
	result, err := bundle.Validate(path)
	if err != nil {
		emitError(bundleErrorCode(err), err.Error(), nil, 1)
	}
	if !result.Valid {
		details, _ := json.Marshal(map[string]any{"issues": result.Issues})
		emitError(rpc.CodeInvalidArgument,
			fmt.Sprintf("bundle validate: %d issue(s) found", len(result.Issues)),
			details, 1)
	}
	emitOK(map[string]any{"valid": true, "events": result.Events})
}

func mustBundlePath(program string, args []string) string {
	var opts struct {
		Path string `arg:"positional,required"`
	}
	if err := parseArg(program, &opts, args); err != nil {
		fatalUsage("%s: %v", program, err)
	}
	return opts.Path
}

// bundleErrorCode maps a bundle package error onto the CLI's rpc error
// codes: a *bundle.LoadError with ErrInvalid means the file was
// readable but not a usable bundle (INVALID_ARGUMENT); anything else —
// including a *bundle.LoadError with ErrIO — is a plain file access
// problem (IO).
func bundleErrorCode(err error) string {
	var le *bundle.LoadError
	if errors.As(err, &le) && le.Kind == bundle.ErrInvalid {
		return rpc.CodeInvalidArgument
	}
	return rpc.CodeIO
}
