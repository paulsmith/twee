package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("wait", runWait)

	registerUsage("wait", `twee wait <subverb> ...
Subverbs:
  wait text --pattern TEXT [--regex] [--timeout <dur>] [--name <name>]
  wait no-text --pattern TEXT [--timeout <dur>] [--name <name>]
  wait stable [--quiet <dur>] [--exclude x,y,w,h]... [--timeout <dur>] [--name <name>]
  wait cell --x <n> --y <n> <predicate flags> [--timeout <dur>] [--name <name>]
  wait cursor --x <n> --y <n> [--timeout <dur>] [--name <name>]
  wait exit [--timeout <dur>] [--name <name>]

Default --timeout is 5s for text/no-text/stable/cell/cursor, 30s for exit.
On timeout the verb exits non-zero with code TIMEOUT. If the session
ends first (child exits, or "twee stop") — text/no-text/cell/cursor exit
non-zero with code SESSION_ENDED instead, so scripts can tell "the app
is slow" from "the app is gone" without string-matching the message.
"wait stable" is the exception: it reports success either way (see
"twee help wait stable").

"wait exit" treats a missing daemon as success (the child has already
exited and the daemon torn down its socket). data.daemon_already_gone
is true in that case.`)
	registerUsage("wait text", `twee wait text --pattern TEXT [--regex] [--timeout <dur>] [--name <name>]
Wait for substr (or regex with --regex) to appear in the viewport.

With --regex, the pattern is matched against the whole viewport joined
by newlines, compiled in multi-line mode: ^ and $ anchor at the start
and end of each line, not just the start/end of the whole viewport.
wait no-text has no --regex option.

Fails with code SESSION_ENDED, not TIMEOUT, if the session ends before
the pattern appears.`)
	registerUsage("wait no-text", `twee wait no-text --pattern TEXT [--timeout <dur>] [--name <name>]
Wait for substr to disappear from the viewport. Fails with code
SESSION_ENDED, not TIMEOUT, if the session ends before it disappears.`)
	registerUsage("wait stable", `twee wait stable [--quiet <dur>] [--exclude x,y,w,h]... [--timeout <dur>] [--name <name>]
Wait for the screen to stop changing for --quiet (default 100ms).
--exclude may be repeated to ignore busy cell rectangles, such as a
spinner in a terminal tail. Each rectangle is x,y,w,h with x/y >= 0
and w/h > 0. Rectangles are clipped to the live viewport, including
after a resize. With --exclude, only changes outside those rectangles
reset the quiet window.

Without --exclude, wait stable uses its existing output-based behavior.

If the session ends while waiting, this reports success rather than
SESSION_ENDED: a screen that will never change again is trivially
"stable". (This differs from wait text/no-text/cursor, which do
distinguish session-ended from timeout.)`)
	registerUsage("wait cursor", `twee wait cursor --x <n> --y <n> [--timeout <dur>] [--name <name>]
Wait for the cursor to land at (x, y). Fails with code SESSION_ENDED,
not TIMEOUT, if the session ends before it does.`)
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
		fatalUsage("wait: missing subverb (text|no-text|stable|cell|cursor|exit)")
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
	case "cell":
		runWaitCell(rest)
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
	args, excludeValues, err := extractWaitExcludes(args)
	if err != nil {
		fatalUsage("wait stable: %v", err)
	}
	var opts struct {
		Name    *string `arg:"--name"`
		Quiet   string  `arg:"--quiet"`
		Timeout string  `arg:"--timeout"`
	}
	if err := parseArg("wait stable", &opts, args); err != nil {
		fatalUsage("wait stable: %v", err)
	}
	exclude := make([]rpc.Rect, len(excludeValues))
	for i, value := range excludeValues {
		rect, err := parseWaitExclude(value)
		if err != nil {
			fatalUsage("wait stable: --exclude %v", err)
		}
		exclude[i] = rect
	}
	callSessionAndEmit("wait stable", opts.Name, rpc.OpWaitStable, rpc.WaitStableArgs{Quiet: opts.Quiet, Timeout: opts.Timeout, Exclude: exclude})
}

// extractWaitExcludes handles this command's repeatable option before
// go-arg sees it. go-arg accepts repeated slice options but keeps only the
// last value, which is unsuitable for independent exclusion rectangles.
func extractWaitExcludes(args []string) ([]string, []string, error) {
	remaining := make([]string, 0, len(args))
	var values []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--exclude":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, nil, fmt.Errorf("--exclude requires a value")
			}
			values = append(values, args[i+1])
			i++
		case strings.HasPrefix(args[i], "--exclude="):
			values = append(values, strings.TrimPrefix(args[i], "--exclude="))
		default:
			remaining = append(remaining, args[i])
		}
	}
	return remaining, values, nil
}

func parseWaitExclude(value string) (rpc.Rect, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return rpc.Rect{}, fmt.Errorf("must be x,y,w,h")
	}
	values := [4]int{}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return rpc.Rect{}, fmt.Errorf("%q: coordinates must be integers", value)
		}
		values[i] = n
	}
	if values[0] < 0 || values[1] < 0 || values[2] <= 0 || values[3] <= 0 {
		return rpc.Rect{}, fmt.Errorf("%q: x/y must be >= 0 and w/h must be > 0", value)
	}
	return rpc.Rect{X: values[0], Y: values[1], W: values[2], H: values[3]}, nil
}

func runWaitCursor(args []string) {
	if err := rejectDuplicateFlags(args, "--x", "--y"); err != nil {
		fatalUsage("wait cursor: %v", err)
	}
	var opts struct {
		Name    *string `arg:"--name"`
		Timeout string  `arg:"--timeout"`
		X       int     `arg:"--x,required"`
		Y       int     `arg:"--y,required"`
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
	if oe, ok := errors.AsType[*net.OpError](err); ok {
		if oe.Op == "dial" {
			return true
		}
	}
	return false
}
