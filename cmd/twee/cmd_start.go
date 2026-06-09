package main

import (
	"fmt"

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
                  trace bundle; finalized automatically at child exit`)
}

func runStart(args []string) {
	opts, err := parseStartArgs(args)
	if err != nil {
		fatalUsage("start: %v", err)
	}
	msg, err := daemonize(opts.name, opts.dir, opts.cmd, opts.cols, opts.rows, opts.env, opts.trace)
	if err != nil {
		code := rpc.CodeIO
		details := msg.ErrorDetails
		message := err.Error()
		if msg.ErrorCode != "" {
			code = msg.ErrorCode
			message = msg.Error
		}
		emitError(code, message, details, 1)
	}
	emitOK(msg)
}

type startOptions struct {
	name  string
	cmd   []string
	cols  int
	rows  int
	dir   string
	env   map[string]string
	trace string
}

func parseStartArgs(args []string) (startOptions, error) {
	before, cmd, err := splitExplicitBoundary("start", args)
	if err != nil {
		return startOptions{}, err
	}
	var parsed struct {
		Name  *string  `arg:"--name"`
		Cols  *string  `arg:"--cols"`
		Rows  *string  `arg:"--rows"`
		Dir   string   `arg:"--dir"`
		Env   []string `arg:"--env,separate"`
		Trace string   `arg:"--trace"`
	}
	if err := requireSeparateValues(before, "--env"); err != nil {
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
	name, err := currentSessionName(nameOptFromPtr(parsed.Name))
	if err != nil {
		return startOptions{}, err
	}
	envOverrides := map[string]string{}
	for _, kv := range parsed.Env {
		k, v, ok := splitKV(kv)
		if !ok {
			return startOptions{}, fmt.Errorf("bad --env value %q (want KEY=VALUE)", kv)
		}
		envOverrides[k] = v
	}
	return startOptions{name: name, cmd: cmd, cols: cols, rows: rows, dir: parsed.Dir, env: envOverrides, trace: parsed.Trace}, nil
}

// multiFlag collects repeated string flags.
type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(s string) error {
	*m = append(*m, s)
	return nil
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
