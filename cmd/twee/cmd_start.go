package main

import (
	"errors"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("start", runStart)
	registerUsage("start", `twee start [daemon options] -- <cmd> [args...]
Spawn a TUI in a new daemon and fork into the background. Prints
{"name": ..., "socket": ..., "pid": ...} on success.

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")
  --cols <int>     initial columns (default 80)
  --rows <int>     initial rows (default 24)
  --dir <path>     child working directory (default: inherit)
  --env KEY=VALUE  environment override (repeatable)
  --trace <path.twee>
                  record the whole session, spawn to teardown, to a
                  trace bundle; finalized automatically at child exit
  --network-capture
                  capture the managed program's IPv4 traffic (Linux; requires --trace)
  --publish-tcp <listen=guest-port>
                  publish LISTEN_IPV4:PORT=GUEST_PORT (repeatable)
  --force          if a live session of this name already exists, stop
                  it first (default grace) instead of failing with
                  ALREADY_RUNNING; adds "replaced":true to the response
                  when it actually stopped something. Stale leftovers
                  (a dead daemon's socket/lock) are already recovered
                  automatically either way.`)
}

func runStart(args []string) {
	opts, err := parseStartArgs(args)
	if err != nil {
		fatalUsage("start: %v", err)
	}
	var replaced bool
	if opts.force {
		replaced = forceStopExisting(opts.name)
	}
	msg, err := daemonize(opts.name, opts.dir, opts.cmd, opts.cols, opts.rows, opts.env, opts.trace, opts.networkCapture, opts.publishTCP)
	if err != nil {
		code := rpc.CodeIO
		var collision *alreadyRunningError
		if errors.As(err, &collision) {
			code = rpc.CodeAlreadyRunning
		}
		details := msg.ErrorDetails
		message := err.Error()
		if msg.ErrorCode != "" {
			code = msg.ErrorCode
			message = msg.Error
		}
		emitError(code, message, details, 1)
	}
	msg.Replaced = replaced
	emitOK(msg)
}

// forceStopExisting is "start --force"'s pre-stop step: if a live daemon
// currently owns name's lock, stop it (default grace) and wait for that
// process to fully exit before returning, so the daemonize call right
// after doesn't race the old daemon's own asynchronous teardown (a stop
// RPC returns as soon as the child is dead, but the daemon process
// releasing its flock and removing its socket/lock happens slightly
// later, in a different goroutine). Returns true only when it actually
// stopped a live session; a stale lock (already unowned — cleaned up by
// stopSession same as a plain "stop" would) or no lock at all reports
// false, since daemonize already recovers those cases on its own
// regardless of --force.
func forceStopExisting(name string) bool {
	pid, hadPID := readLockPID(name)
	r := stopSession(name, rpc.StopArgs{})
	replaced := r.errCode == "" && r.data["stopped"] == true
	if hadPID {
		waitForPIDExit(pid, 3*time.Second)
	}
	return replaced
}

type startOptions struct {
	name           string
	cmd            []string
	cols           int
	rows           int
	dir            string
	env            map[string]string
	trace          string
	force          bool
	networkCapture bool
	publishTCP     []engine.TCPPublication
}

func parseStartArgs(args []string) (startOptions, error) {
	before, cmd, err := splitExplicitBoundary("start", args)
	if err != nil {
		return startOptions{}, err
	}
	var parsed struct {
		Name           *string  `arg:"--name"`
		Cols           *string  `arg:"--cols"`
		Rows           *string  `arg:"--rows"`
		Dir            string   `arg:"--dir"`
		Env            []string `arg:"--env,separate"`
		Trace          string   `arg:"--trace"`
		Force          bool     `arg:"--force"`
		NetworkCapture bool     `arg:"--network-capture"`
		PublishTCP     []string `arg:"--publish-tcp,separate"`
	}
	if err := requireSeparateValues(before, "--env"); err != nil {
		return startOptions{}, err
	}
	if err := requireSeparateValues(before, "--publish-tcp"); err != nil {
		return startOptions{}, err
	}
	if err := parseArg("start", &parsed, before); err != nil {
		return startOptions{}, err
	}
	cols, err := positiveIntFlagOrDefault("--cols", parsed.Cols, 80)
	if err != nil {
		return startOptions{}, err
	}
	rows, err := positiveIntFlagOrDefault("--rows", parsed.Rows, 24)
	if err != nil {
		return startOptions{}, err
	}
	name, err := resolveSessionNamePtr(parsed.Name)
	if err != nil {
		return startOptions{}, err
	}
	envOverrides, err := parseEnvOverrides(parsed.Env)
	if err != nil {
		return startOptions{}, err
	}
	trace, err := absOutPath(parsed.Trace)
	if err != nil {
		return startOptions{}, err
	}
	pubs, err := parseNetworkCaptureFlags(parsed.NetworkCapture, parsed.PublishTCP, trace, "--trace")
	if err != nil {
		return startOptions{}, err
	}
	return startOptions{name: name, cmd: cmd, cols: cols, rows: rows, dir: parsed.Dir, env: envOverrides, trace: trace, force: parsed.Force, networkCapture: parsed.NetworkCapture, publishTCP: pubs}, nil
}
