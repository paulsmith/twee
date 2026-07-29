package main

import (
	"errors"
	"io/fs"
	"net"
	"os"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("wait", runWait)

	registerUsage("wait", `twee wait <subverb> ...
Subverbs:
  wait text --pattern TEXT [--regex] [--timeout <dur>] [--name <name>]
  wait no-text --pattern TEXT [--timeout <dur>] [--name <name>]
  wait stable [--quiet <dur>] [--timeout <dur>] [--name <name>]
  wait cursor <x> <y> [--timeout <dur>] [--name <name>]
  wait exit [--timeout <dur>] [--name <name>]

Default --timeout is 5s for text/no-text/stable/cursor, 30s for exit.
On timeout the verb exits non-zero with code TIMEOUT.

"wait exit" treats a missing daemon as success (the child has already
exited and the daemon torn down its socket). data.daemon_already_gone
is true in that case.`)
	registerUsage("wait text", `twee wait text --pattern TEXT [--regex] [--timeout <dur>] [--name <name>]
Wait for substr (or regex with --regex) to appear in the viewport.

With --regex, the pattern is matched against the whole viewport joined
by newlines, compiled in multi-line mode: ^ and $ anchor at the start
and end of each line, not just the start/end of the whole viewport.
wait no-text has no --regex option.`)
	registerUsage("wait no-text", `twee wait no-text --pattern TEXT [--timeout <dur>] [--name <name>]
Wait for substr to disappear from the viewport.`)
	registerUsage("wait stable", `twee wait stable [--quiet <dur>] [--timeout <dur>] [--name <name>]
Wait for the screen to stop changing for --quiet (default 100ms).
Will hang on apps with always-running spinners; use "wait text"
instead for those.`)
	registerUsage("wait cursor", `twee wait cursor <x> <y> [--timeout <dur>] [--name <name>]
Wait for the cursor to land at (x, y).`)
	registerUsage("wait exit", `twee wait exit [--timeout <dur>] [--name <name>]
Wait for the child process to exit. Default --timeout is 30s. If the
daemon socket is already gone, returns success with
{exit_code: null, daemon_already_gone: true}.

wait exit returns only after the session's artifacts are durable: an
active trace is finalized to its bundle first, and the response carries
its path as {"trace_path": ...}.`)
}

func runWait(args []string) {
	if len(args) == 0 {
		fatalUsage("wait: missing subverb (text|no-text|stable|cursor|exit)")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "text":
		runWaitText(rest)
	case "no-text":
		runWaitNoText(rest)
	case "stable":
		runWaitStable(rest)
	case "cursor":
		runWaitCursor(rest)
	case "exit":
		runWaitExit(rest)
	default:
		fatalUsage("wait: unknown subverb %q", sub)
	}
}

func runWaitText(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout"`
		Regex   bool    `arg:"--regex"`
		Pattern string  `arg:"--pattern,required"`
	}
	if err := parseArg("wait text", &opts, args); err != nil {
		fatalUsage("wait text: %v", err)
	}
	callSessionAndEmit("wait text", opts.Name, rpc.OpWaitText, rpc.WaitTextArgs{Text: opts.Pattern, Regex: opts.Regex, Timeout: opts.Timeout})
}

func runWaitNoText(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout"`
		Pattern string  `arg:"--pattern,required"`
	}
	if err := parseArg("wait no-text", &opts, args); err != nil {
		fatalUsage("wait no-text: %v", err)
	}
	callSessionAndEmit("wait no-text", opts.Name, rpc.OpWaitNoText, rpc.WaitNoTextArgs{Text: opts.Pattern, Timeout: opts.Timeout})
}

func runWaitStable(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Quiet   string  `arg:"--quiet"`
		Timeout string  `arg:"--timeout"`
	}
	if err := parseArg("wait stable", &opts, args); err != nil {
		fatalUsage("wait stable: %v", err)
	}
	callSessionAndEmit("wait stable", opts.Name, rpc.OpWaitStable, rpc.WaitStableArgs{Quiet: opts.Quiet, Timeout: opts.Timeout})
}

func runWaitCursor(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout"`
		X       int     `arg:"positional,required"`
		Y       int     `arg:"positional,required"`
	}
	if err := parseArg("wait cursor", &opts, args); err != nil {
		fatalUsage("wait cursor: %v", err)
	}
	callSessionAndEmit("wait cursor", opts.Name, rpc.OpWaitCursor, rpc.WaitCursorArgs{X: opts.X, Y: opts.Y, Timeout: opts.Timeout})
}

func runWaitExit(args []string) {
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout" default:"30s"`
	}
	if err := parseArg("wait exit", &opts, args); err != nil {
		fatalUsage("wait exit: %v", err)
	}
	name := mustResolveSessionNamePtr("wait exit", opts.Name)
	if !daemonReachable(name) {
		emitOK(map[string]any{"exit_code": nil, "daemon_already_gone": true})
		return
	}
	resp, err := callDaemon(name, rpc.OpWaitExit, rpc.WaitExitArgs{Timeout: opts.Timeout})
	if err != nil {
		// Race: daemon torn down between the reachability probe and the RPC.
		if isDaemonGone(err) {
			emitOK(map[string]any{"exit_code": nil, "daemon_already_gone": true})
			return
		}
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	emitOKRaw(resp.Data)
}

// daemonReachable reports whether the named daemon's socket exists on
// disk. We don't dial here — a non-existent socket is the only state
// we treat as "definitely already gone"; transient dial failures fall
// through to the RPC and surface real errors.
func daemonReachable(name string) bool {
	sock, err := socketPath(name)
	if err != nil {
		return false
	}
	_, statErr := os.Stat(sock)
	return statErr == nil
}

// isDaemonGone returns true for errors that mean the daemon vanished
// between probe and dial — typically ENOENT on the socket path.
func isDaemonGone(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		if oe.Op == "dial" {
			return true
		}
	}
	return false
}
