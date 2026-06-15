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
daemon's socket and lock file. Returns {"name": ..., "stopped": true}.`)
}

func runStop(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("stop", &opts, args); err != nil {
		fatalUsage("stop: %v", err)
	}
	name := mustResolveSessionNamePtr("stop", opts.Name)
	resp, err := callDaemon(name, rpc.OpStop, nil)
	if err != nil {
		code := transportErrorCode(err)
		if code == rpc.CodeNotFound {
			cleanupStaleSession(name)
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
// proves no live daemon still owns it.
func cleanupStaleSession(name string) {
	lp, err := lockPath(name)
	if err != nil {
		return
	}
	lf, err := os.OpenFile(lp, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer lf.Close()
	if syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return // a live daemon still holds it
	}
	_ = os.Remove(lp)
	if sp, err := socketPath(name); err == nil {
		_ = os.Remove(sp)
	}
}
