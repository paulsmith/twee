package main

import (
	"os"
	"strings"
	"syscall"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("stop", runStop)
	registerUsage("stop", `twee stop [--name <name>] [--grace <dur>]
twee stop --all [--grace <dur>]
SIGTERM the child, wait for --grace (default 250ms), escalate to
SIGKILL, then remove the daemon's socket and lock file. Returns
{"name": ..., "stopped": true}.

--grace overrides the SIGTERM-to-SIGKILL escalation window. "0"
means SIGKILL immediately, skipping the wait. A negative grace is
INVALID_ARGUMENT.

--all stops every live session and cleans up every stale one instead
of targeting a single name; it is mutually exclusive with --name (or a
global --name). Returns {"ok":true,"data":[{...}, ...]}, each element
shaped like a single stop's data plus "name" — [] when no sessions
exist at all, still exit 0.

If the session's socket file exists but the daemon is unreachable (it
was killed without cleaning up, e.g. kill -9), stop instead removes the
stale socket and lock file and returns {"name": ..., "stopped": false,
"stale_cleaned": true} (exit 0). A name with no socket file at all is
still NOT_FOUND (--name only; --all simply omits it).`)
}

func runStop(args []string) {
	var opts struct {
		Name  *string `arg:"--name"`
		All   bool    `arg:"--all"`
		Grace *string `arg:"--grace"`
	}
	if err := parseArg("stop", &opts, args); err != nil {
		fatalUsage("stop: %v", err)
	}
	var graceArgs rpc.StopArgs
	if opts.Grace != nil {
		graceArgs.Grace = *opts.Grace
	}
	if opts.All {
		if opts.Name != nil || rootGlobalName.Present {
			fatalUsage("stop: --all is mutually exclusive with --name")
		}
		emitOK(stopAll(graceArgs))
		return
	}
	name := mustResolveSessionNamePtr("stop", opts.Name)
	r := stopSession(name, graceArgs)
	if r.errCode != "" {
		emitError(r.errCode, r.errMessage, r.errDetails, 1)
	}
	emitOK(r.data)
}

// stopOutcome is the result of stopping one named session: either data
// ready to report (a successful stop or a stale cleanup) or an error
// triple ready for emitError.
type stopOutcome struct {
	data       map[string]any
	errCode    string
	errMessage string
	errDetails []byte
}

// stopSession runs the same stop-then-cleanup sequence "twee stop
// --name X" always has, factored out so both the single-session path and
// "stop --all" (which calls this once per discovered session) share it.
func stopSession(name string, args rpc.StopArgs) stopOutcome {
	// Captured before cleanupStaleSession can remove the socket file, so
	// it reflects whether one existed at all versus never having
	// existed for this name.
	sockExisted := daemonReachable(name)
	resp, err := callDaemon(name, rpc.OpStop, args)
	if err != nil {
		code := transportErrorCode(err)
		if code == rpc.CodeNotFound {
			cleaned := cleanupStaleSession(name)
			if sockExisted && cleaned {
				return stopOutcome{data: map[string]any{"name": name, "stopped": false, "stale_cleaned": true}}
			}
		}
		return stopOutcome{errCode: code, errMessage: err.Error()}
	}
	if !resp.OK {
		return stopOutcome{errCode: resp.Error.Code, errMessage: resp.Error.Message, errDetails: resp.Error.Details}
	}
	if sp, err := socketPath(name); err == nil {
		_ = os.Remove(sp)
	}
	if lp, err := lockPath(name); err == nil {
		_ = os.Remove(lp)
	}
	return stopOutcome{data: map[string]any{"name": name, "stopped": true}}
}

// stopAll stops every live session and cleans up every stale one found
// in the state dir, the same way "ls" discovers sessions to report (by
// scanning for "*.sock" entries). Unlike the single-session path, a
// per-session failure doesn't abort the whole command or change the
// exit code — it's folded into that session's element as an "error"
// field instead, so one uncooperative session can't hide the results
// for the rest.
func stopAll(args rpc.StopArgs) []any {
	out := []any{}
	dir, err := stateDir()
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sock")
		r := stopSession(name, args)
		if r.errCode != "" {
			out = append(out, map[string]any{
				"name":    name,
				"stopped": false,
				"error":   map[string]any{"code": r.errCode, "message": r.errMessage},
			})
			continue
		}
		out = append(out, r.data)
	}
	return out
}

// cleanupStaleSession removes the lock and socket files of a session
// whose daemon is unreachable, but only after acquiring the lock's flock
// proves no live daemon still owns it. Returns true if it removed stale
// state; false if there was nothing to clean (no lock file) or a live
// daemon still holds the lock, in which case nothing was touched.
func cleanupStaleSession(name string) bool {
	lp, err := lockPath(name)
	if err != nil {
		return false
	}
	lf, err := os.OpenFile(lp, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer lf.Close()
	if syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return false // a live daemon still holds it
	}
	_ = os.Remove(lp)
	if sp, err := socketPath(name); err == nil {
		_ = os.Remove(sp)
	}
	return true
}

// isSessionStale reports whether name's lock file exists but is not
// currently held by any live daemon (its flock is free), meaning the
// daemon crashed or was killed without removing its state. Unlike
// cleanupStaleSession, it is read-only: it takes and immediately
// releases the flock (via the deferred close) without removing
// anything. Used by "ls" to flag abandoned sessions instead of
// silently omitting them.
func isSessionStale(name string) bool {
	lp, err := lockPath(name)
	if err != nil {
		return false
	}
	lf, err := os.OpenFile(lp, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer lf.Close()
	return syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
