package main

import (
	"os"
	"syscall"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("stop", runStop)
	registerUsage("stop", `twee stop [--name <name>]
SIGTERM the child, wait 250ms, escalate to SIGKILL, then remove the
daemon's socket and lock file. Returns {"name": ..., "stopped": true}.

If the session's socket file exists but the daemon is unreachable (it
was killed without cleaning up, e.g. kill -9), stop instead removes the
stale socket and lock file and returns {"name": ..., "stopped": false,
"stale_cleaned": true} (exit 0). A name with no socket file at all is
still NOT_FOUND.`)
}

func runStop(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("stop", &opts, args); err != nil {
		fatalUsage("stop: %v", err)
	}
	name := mustResolveSessionNamePtr("stop", opts.Name)
	// Captured before cleanupStaleSession can remove the socket file, so
	// it reflects whether one existed at all versus never having
	// existed for this name.
	sockExisted := daemonReachable(name)
	resp, err := callDaemon(name, rpc.OpStop, nil)
	if err != nil {
		code := transportErrorCode(err)
		if code == rpc.CodeNotFound {
			cleaned := cleanupStaleSession(name)
			if sockExisted && cleaned {
				emitOK(map[string]any{"name": name, "stopped": false, "stale_cleaned": true})
				return
			}
		}
		emitError(code, err.Error(), nil, 1)
	}
	if !resp.OK {
		emitError(resp.Error.Code, resp.Error.Message, resp.Error.Details, 1)
	}
	if sp, err := socketPath(name); err == nil {
		_ = os.Remove(sp)
	}
	if lp, err := lockPath(name); err == nil {
		_ = os.Remove(lp)
	}
	emitOK(map[string]any{"name": name, "stopped": true})
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
