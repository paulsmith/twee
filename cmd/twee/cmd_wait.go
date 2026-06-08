package main

import (
	"errors"
	"flag"
	"io/fs"
	"net"
	"os"
	"strconv"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("wait", runWait)

	registerUsage("wait", `twee wait <subverb> ...
Subverbs:
  wait text <substr> [-regex] [-timeout <dur>] [-name <name>]
  wait no-text <substr> [-timeout <dur>] [-name <name>]
  wait stable [-quiet <dur>] [-timeout <dur>] [-name <name>]
  wait cursor <x> <y> [-timeout <dur>] [-name <name>]
  wait exit [-timeout <dur>] [-name <name>]

Default -timeout is 5s for text/no-text/stable/cursor, 30s for exit.
On timeout the verb exits non-zero with code TIMEOUT.

"wait exit" treats a missing daemon as success (the child has already
exited and the daemon torn down its socket). data.daemon_already_gone
is true in that case.`)
	registerUsage("wait text", `twee wait text <substr> [-regex] [-timeout <dur>] [-name <name>]
Wait for substr (or regex with -regex) to appear in the viewport.`)
	registerUsage("wait no-text", `twee wait no-text <substr> [-timeout <dur>] [-name <name>]
Wait for substr to disappear from the viewport.`)
	registerUsage("wait stable", `twee wait stable [-quiet <dur>] [-timeout <dur>] [-name <name>]
Wait for the screen to stop changing for -quiet (default 100ms).
Will hang on apps with always-running spinners; use "wait text"
instead for those.`)
	registerUsage("wait cursor", `twee wait cursor <x> <y> [-timeout <dur>] [-name <name>]
Wait for the cursor to land at (x, y).`)
	registerUsage("wait exit", `twee wait exit [-timeout <dur>] [-name <name>]
Wait for the child process to exit. Default -timeout is 30s. If the
daemon socket is already gone, returns success with
{exit_code: null, daemon_already_gone: true}.`)
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
	fs := flag.NewFlagSet("wait text", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	timeout := fs.String("timeout", "", "duration; default = engine default")
	regex := fs.Bool("regex", false, "treat text as regex")
	if err := fs.Parse(args); err != nil {
		fatalUsage("wait text: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("wait text: expected one text argument")
	}
	callAndEmit(*name, rpc.OpWaitText, rpc.WaitTextArgs{Text: rest[0], Regex: *regex, Timeout: *timeout})
}

func runWaitNoText(args []string) {
	fs := flag.NewFlagSet("wait no-text", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	timeout := fs.String("timeout", "", "duration")
	if err := fs.Parse(args); err != nil {
		fatalUsage("wait no-text: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("wait no-text: expected one text argument")
	}
	callAndEmit(*name, rpc.OpWaitNoText, rpc.WaitNoTextArgs{Text: rest[0], Timeout: *timeout})
}

func runWaitStable(args []string) {
	fs := flag.NewFlagSet("wait stable", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	quiet := fs.String("quiet", "", "quiet window")
	timeout := fs.String("timeout", "", "overall timeout")
	if err := fs.Parse(args); err != nil {
		fatalUsage("wait stable: %v", err)
	}
	callAndEmit(*name, rpc.OpWaitStable, rpc.WaitStableArgs{Quiet: *quiet, Timeout: *timeout})
}

func runWaitCursor(args []string) {
	fs := flag.NewFlagSet("wait cursor", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	timeout := fs.String("timeout", "", "duration")
	if err := fs.Parse(args); err != nil {
		fatalUsage("wait cursor: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 2 {
		fatalUsage("wait cursor: expected x y")
	}
	x, err1 := strconv.Atoi(rest[0])
	y, err2 := strconv.Atoi(rest[1])
	if err1 != nil || err2 != nil {
		fatalUsage("wait cursor: x and y must be integers")
	}
	callAndEmit(*name, rpc.OpWaitCursor, rpc.WaitCursorArgs{X: x, Y: y, Timeout: *timeout})
}

func runWaitExit(args []string) {
	flags := flag.NewFlagSet("wait exit", flag.ExitOnError)
	name := flags.String("name", "default", "session name")
	timeout := flags.String("timeout", "30s", "duration; default 30s")
	if err := flags.Parse(args); err != nil {
		fatalUsage("wait exit: %v", err)
	}
	if !daemonReachable(*name) {
		emitOK(map[string]any{"exit_code": nil, "daemon_already_gone": true})
		return
	}
	resp, err := callDaemon(*name, rpc.OpWaitExit, rpc.WaitExitArgs{Timeout: *timeout})
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
