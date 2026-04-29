package main

import (
	"flag"
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

	registerUsage("type", `twee type <text...>
Write literal text to the PTY. Multiple positional arguments are
joined with single spaces. Use this for ALL printable characters
including single letters; "twee key i" will not work.

Flags:
  -name <name>     session name (default "default")`)

	registerUsage("key", `twee key <name>
Send one named key. Valid names:
  Enter, Escape (Esc), Tab, Backspace, Delete (Del),
  Up, Down, Left, Right, Home, End, PageUp (PgUp), PageDown (PgDn),
  Ctrl+<letter>   (e.g. Ctrl+C, Ctrl+D, Ctrl+Z)

For literal characters or strings, use "twee type" instead.

Flags:
  -name <name>     session name (default "default")`)

	registerUsage("keys", `twee keys <name> [<name>...]
Convenience for sending multiple named keys in sequence. Equivalent
to N successive "twee key" calls. Same naming rules as "twee key".

Flags:
  -name <name>     session name (default "default")`)

	registerUsage("paste", `twee paste <text...>
Send text wrapped in bracketed-paste markers (DEC mode 2004). If the
TUI hasn't enabled mode 2004, the markers will appear as literal
input. Multiple args are joined with single spaces.

Flags:
  -name <name>     session name (default "default")`)

	registerUsage("signal", `twee signal <name>
Send a POSIX signal to the child process (not the daemon). Examples:
SIGINT, SIGTERM, SIGWINCH, SIGUSR1.

Flags:
  -name <name>     session name (default "default")`)
}

func runType(args []string) {
	fs := flag.NewFlagSet("type", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("type: %v", err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fatalUsage("type: missing text")
	}
	text := strings.Join(rest, " ")
	callAndEmit(*name, rpc.OpType, rpc.TypeArgs{Text: text})
}

func runKey(args []string) {
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("key: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("key: expected exactly one key name")
	}
	if _, err := input.Parse(rest[0]); err != nil {
		fatalUsage("%s", keyErrorHint(rest[0]))
	}
	callAndEmit(*name, rpc.OpKey, rpc.KeyArgs{Key: rest[0]})
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
	fs := flag.NewFlagSet("keys", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("keys: %v", err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fatalUsage("keys: missing keys")
	}
	for _, k := range rest {
		if _, err := input.Parse(k); err != nil {
			fatalUsage("keys: %s", keyErrorHint(k))
		}
	}
	for _, k := range rest {
		callOnly(*name, rpc.OpKey, rpc.KeyArgs{Key: k})
	}
	emitOK(map[string]int{"sent": len(rest)})
}

func runPaste(args []string) {
	fs := flag.NewFlagSet("paste", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("paste: %v", err)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fatalUsage("paste: missing text")
	}
	callAndEmit(*name, rpc.OpPaste, rpc.PasteArgs{Text: strings.Join(rest, " ")})
}

func runSignal(args []string) {
	fs := flag.NewFlagSet("signal", flag.ExitOnError)
	name := fs.String("name", "default", "session name")
	if err := fs.Parse(args); err != nil {
		fatalUsage("signal: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fatalUsage("signal: expected exactly one signal name")
	}
	callAndEmit(*name, rpc.OpSignal, rpc.SignalArgs{Name: rest[0]})
}
