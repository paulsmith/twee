package main

import (
	"fmt"
	"strings"

	"github.com/paulsmith/research/twee/internal/input"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	register("type", runType)
	register("key", runKey)
	register("keys", runKeys)
	register("paste", runPaste)
	register("signal", runSignal)

	registerUsage("type", `twee type [client options] -- <text...>
Write literal text to the PTY. Multiple positional arguments are
joined with single spaces. Use this for ALL printable characters
including single letters; "twee key i" will not work.

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")`)

	registerUsage("key", `twee key <name> [--name <session>]
Send one named key. Valid names:
  Enter, Escape (Esc), Tab, Backspace, Delete (Del),
  Up, Down, Left, Right, Home, End, PageUp (PgUp), PageDown (PgDn),
  Ctrl+<letter>   (e.g. Ctrl+C, Ctrl+D, Ctrl+Z)

For literal characters or strings, use "twee type" instead.

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")`)

	registerUsage("keys", `twee keys <name> [<name>...] [--name <session>]
Convenience for sending multiple named keys in sequence. Equivalent
to N successive "twee key" calls. Same naming rules as "twee key".

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")`)

	registerUsage("paste", `twee paste [client options] -- <text...>
Send text wrapped in bracketed-paste markers (DEC mode 2004). If the
TUI hasn't enabled mode 2004, the markers will appear as literal
input. Multiple args are joined with single spaces.

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")`)

	registerUsage("signal", `twee signal <name> [--name <session>]
Send a POSIX signal to the child process (not the daemon). Examples:
SIGINT, SIGTERM, SIGWINCH, SIGUSR1.

Flags:
  --name <name>    session name (default: TWEE_SESSION or "default")`)
}

func runType(args []string) {
	before, payload, err := splitExplicitBoundary("type", args)
	if err != nil {
		fatalUsage("type: %v", err)
	}
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("type", &opts, before); err != nil {
		fatalUsage("type: %v", err)
	}
	callAndEmit(mustCurrentSessionName("type", nameOptFromPtr(opts.Name)), rpc.OpType, rpc.TypeArgs{Text: strings.Join(payload, " ")})
}

func runKey(args []string) {
	var opts struct {
		Name *string `arg:"--name"`
		Key  string  `arg:"positional,required"`
	}
	if err := parseArg("key", &opts, args); err != nil {
		fatalUsage("key: %v", err)
	}
	if _, err := input.Parse(opts.Key); err != nil {
		fatalUsage("%s", keyErrorHint(opts.Key))
	}
	callAndEmit(mustCurrentSessionName("key", nameOptFromPtr(opts.Name)), rpc.OpKey, rpc.KeyArgs{Key: opts.Key})
}

// keyErrorHint produces a useful message when a "key" argument doesn't
// parse. If the user passed a single printable ASCII character it
// almost certainly means they wanted "twee type"; say so.
func keyErrorHint(arg string) string {
	if len(arg) == 1 {
		c := arg[0]
		if c >= 0x20 && c < 0x7f {
			return fmt.Sprintf("key: %q is not a named key — for literal characters use: twee type %q", arg, arg)
		}
	}
	return fmt.Sprintf("key: %q is not a named key. Valid names: Enter, Escape, Tab, Backspace, Delete, Up, Down, Left, Right, Home, End, PageUp, PageDown, Ctrl+<letter>. See: twee help key", arg)
}

func runKeys(args []string) {
	var opts struct {
		Name *string  `arg:"--name"`
		Keys []string `arg:"positional,required"`
	}
	if err := parseArg("keys", &opts, args); err != nil {
		fatalUsage("keys: %v", err)
	}
	for _, k := range opts.Keys {
		if _, err := input.Parse(k); err != nil {
			fatalUsage("keys: %s", keyErrorHint(k))
		}
	}
	name := mustCurrentSessionName("keys", nameOptFromPtr(opts.Name))
	for _, k := range opts.Keys {
		callOnly(name, rpc.OpKey, rpc.KeyArgs{Key: k})
	}
	emitOK(map[string]int{"sent": len(opts.Keys)})
}

func runPaste(args []string) {
	before, payload, err := splitExplicitBoundary("paste", args)
	if err != nil {
		fatalUsage("paste: %v", err)
	}
	var opts struct {
		Name *string `arg:"--name"`
	}
	if err := parseArg("paste", &opts, before); err != nil {
		fatalUsage("paste: %v", err)
	}
	callAndEmit(mustCurrentSessionName("paste", nameOptFromPtr(opts.Name)), rpc.OpPaste, rpc.PasteArgs{Text: strings.Join(payload, " ")})
}

func runSignal(args []string) {
	var opts struct {
		Name   *string `arg:"--name"`
		Signal string  `arg:"positional,required"`
	}
	if err := parseArg("signal", &opts, args); err != nil {
		fatalUsage("signal: %v", err)
	}
	callAndEmit(mustCurrentSessionName("signal", nameOptFromPtr(opts.Name)), rpc.OpSignal, rpc.SignalArgs{Name: opts.Signal})
}
