package main

import (
	"encoding/json"
	"net"
)

func init() {
	register("do", runDo)
	registerUsage("do", `twee do [--script <path|->] [--emit results] [--name <name>]
Execute a JSON op script — same format as "twee run" — against an
already-running session instead of spawning an ephemeral one. Useful
for batching many ops against a long-lived session in one process
launch instead of one "twee <verb>" invocation per op.

Session resolution is the same as every other daemon verb: per-command
--name, global --name, $TWEE_SESSION, then "default". No matching
session fails with NOT_FOUND, same as any other verb.

Flags:
  --script <path>  path to script JSON; "-" or empty reads stdin
  --emit results   stream NDJSON op responses instead of one summary
  --name <name>    target session (default: $TWEE_SESSION or "default")

The script is a JSON array of RPC bodies (op + args), using wire op
names (e.g. "wait_text", not "wait text") — identical to "twee run"'s
script format; scripts written by "twee wrap --script-out" work unchanged.
Relative paths in operation arguments (screenshot.out, trace_start.out,
and diff.against) are resolved from the twee do client's working directory,
not the daemon's or managed program's working directory.
Ops like "stop" or "wait_exit" are not special-cased: they do whatever
they normally do, including ending the session. In NDJSON mode, a failing
operation is the terminal record and exits 1; no final summary is appended.`)
}

func runDo(args []string) {
	var opts struct {
		Name       *string `arg:"--name"`
		ScriptPath string  `arg:"--script"`
		Emit       string  `arg:"--emit"`
	}
	if err := parseArg("do", &opts, args); err != nil {
		fatalUsage("do: %v", err)
	}
	if opts.Emit != "" && opts.Emit != "results" {
		fatalUsage("do: --emit must be results (got %q)", opts.Emit)
	}
	name := mustResolveSessionNamePtr("do", opts.Name)

	ops := loadScript(opts.ScriptPath)

	dial := func() (net.Conn, error) { return dialSession(name) }

	// An empty (or all-no-op) script would otherwise never dial at all,
	// so a missing/dead session would silently report {"ops":0} instead
	// of NOT_FOUND. Probe once up front to match every other verb's
	// "no session -> NOT_FOUND" behavior regardless of script content.
	if len(ops) == 0 {
		c, err := dial()
		if err != nil {
			emitError(transportErrorCode(err), err.Error(), dialErrorDetails(err), 1)
		}
		_ = c.Close()
	}

	emitResults := opts.Emit == "results"
	fail := func(code, msg string, details json.RawMessage) {
		emitError(code, msg, details, 1)
	}
	runOpScript(ops, dial, emitResults, func() {}, fail)
	if !emitResults {
		emitOK(map[string]any{"ops": len(ops)})
	}
}
