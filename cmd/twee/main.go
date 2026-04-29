// Command twee is a CLI for driving terminal UIs. See
// docs/superpowers/specs/2026-04-28-twee-cli-design.md for the design.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if inDaemonMode() {
		runDaemonChild()
		return // unreachable
	}
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	verb := os.Args[1]
	args := os.Args[2:]

	if h := dispatch[verb]; h != nil {
		h(args)
		return
	}

	switch verb {
	case "version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		if len(args) > 0 {
			printVerbHelp(os.Stdout, args)
			return
		}
		printUsage(os.Stdout)
	case "completion":
		runCompletion(args)
	default:
		fmt.Fprintf(os.Stderr, "twee: unknown subcommand %q\n", verb)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

// dispatch is the verb table. Other files (cmd_*.go) populate it via
// init() so M1 builds without their dependencies.
var dispatch = map[string]func(args []string){}

func register(verb string, fn func(args []string)) { dispatch[verb] = fn }

// usages holds per-verb help text, populated by cmd_*.go init() funcs
// via registerUsage. Multi-word verbs (e.g. "wait text") are keyed by
// their full verb path joined with a single space.
var usages = map[string]string{}

func registerUsage(verb, help string) { usages[verb] = help }

func printVerbHelp(w io.Writer, args []string) {
	key := strings.Join(args, " ")
	if h, ok := usages[key]; ok {
		fmt.Fprintln(w, h)
		return
	}
	// Fall back to the first word — e.g. "help wait" prints the wait
	// overview when no specific subverb was given.
	if h, ok := usages[args[0]]; ok {
		fmt.Fprintln(w, h)
		return
	}
	fmt.Fprintf(os.Stderr, "twee: no help available for %q\n", key)
	os.Exit(2)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `twee — drive TUIs from the shell.

Usage: twee <verb> [positional args...] [-flag value ...]

Lifecycle:
  start <cmd> [args...]         Spawn a TUI in a daemon
  stop                          Stop the running daemon
  ls                            List running daemons
  status                        Print the status of a daemon
  run <cmd> [args...] -script   Single-shot ephemeral session

Input:    type | key | keys | paste | signal
Queries:  text | lines | cell | region | cursor | find
          size | title | mode | scrollback | snapshot
State:    resize | screenshot | record | diff
Waits:    wait text | wait no-text | wait stable | wait cursor | wait exit
Misc:     sleep | version | help | completion

Common flags (per verb; both -name and --name are accepted):
  -name <name>      Target a named daemon (default: "default")
  -timeout <dur>    Override timeout for wait verbs

Flags must appear AFTER the verb (they're parsed by each verb's flag
set, not globally). "twee --name foo status" fails; write
"twee status -name foo".

For literal characters use "twee type"; "twee key" only accepts named
keys (Enter, Down, Ctrl+C, ...). "twee key i" fails — use "twee type i".

Output is JSON by default:
  {"ok": true, "data": {...}}            on success
  {"ok": false, "error": {...}}          on failure

Run "twee help <verb>" for per-verb usage (e.g. "twee help start",
"twee help wait text"). Spec:
  docs/superpowers/specs/2026-04-28-twee-cli-design.md`)
}

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "twee completion: missing shell argument (bash|zsh|fish)")
		os.Exit(2)
	}
	switch args[0] {
	case "bash", "zsh", "fish":
		fmt.Println("# twee completion: not yet generated")
	default:
		fmt.Fprintf(os.Stderr, "twee completion: unknown shell %q\n", args[0])
		os.Exit(2)
	}
}

func inDaemonMode() bool { return inDaemonModeReal() }
func runDaemonChild()    { runDaemonChildReal() }
