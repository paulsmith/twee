package main

import (
	"flag"
	"fmt"
)

func init() {
	register("start", runStart)
	registerUsage("start", `twee start <cmd> [args...] [flags]
Spawn a TUI in a new daemon and fork into the background. Prints
{"name": ..., "socket": ..., "pid": ...} on success.

Flags:
  -name <name>     session name (default "default")
  -cols <int>      initial columns (default 80)
  -rows <int>      initial rows (default 24)
  -dir <path>      child working directory (default: inherit)
  -env KEY=VALUE   environment override (repeatable)`)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	cols := fs.Int("cols", 80, "initial columns")
	rows := fs.Int("rows", 24, "initial rows")
	dir := fs.String("dir", "", "working directory of child (empty = inherit)")
	var envFlags multiFlag
	fs.Var(&envFlags, "env", "environment override KEY=VALUE (repeatable)")
	if err := fs.Parse(args); err != nil {
		fatalUsage("start: %v", err)
	}
	cmd := fs.Args()
	if len(cmd) == 0 {
		fatalUsage("start: missing command")
	}
	envOverrides := map[string]string{}
	for _, kv := range envFlags {
		k, v, ok := splitKV(kv)
		if !ok {
			fatalUsage("start: bad --env value %q (want KEY=VALUE)", kv)
		}
		envOverrides[k] = v
	}
	msg, err := daemonize(*name, *dir, cmd, *cols, *rows, envOverrides)
	if err != nil {
		emitError("IO", err.Error(), nil, 1)
	}
	emitOK(msg)
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
