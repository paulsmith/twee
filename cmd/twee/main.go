// Command twee drives terminal UIs from shell scripts and tests.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func init() {
	registerUsage("completion", `twee completion <bash|zsh|fish>
Print a shell completion script. Completion generation is currently a
placeholder.`)
	registerUsage("help", `twee help [<verb> [<subverb>...]]
Print top-level help or per-command usage.`)
	registerUsage("version", `twee version
Print the twee version.`)
}

func main() {
	if inDaemonMode() {
		runDaemonChild()
		return // unreachable
	}
	root, err := parseRootArgs(os.Args[1:])
	if err != nil {
		fatalUsage("%v", err)
	}
	rootGlobalName = root.GlobalName
	if root.Help {
		if root.HelpKey == "" {
			printUsage(os.Stdout)
			return
		}
		printVerbHelp(os.Stdout, strings.Split(root.HelpKey, " "))
		return
	}
	if root.Verb == "" {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	verb := root.Verb
	args := root.Args

	if h := dispatch[verb]; h != nil {
		h(args)
		return
	}

	switch verb {
	case "version":
		fmt.Println(Version)
	case "help":
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
	fmt.Fprintln(w, `twee - drive TUIs from the shell.

Usage: twee [--name <session>] <verb> [args...]

Commands:`)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range commandSummaries {
		fmt.Fprintf(tw, "  %s\t%s\n", cmd.name, cmd.summary)
	}
	_ = tw.Flush()
	fmt.Fprintln(w, `
Wait commands:
  wait cursor     Wait for the cursor to reach a position
  wait exit       Wait for the child process to exit
  wait no-text    Wait for text to disappear
  wait stable     Wait for the screen to stop changing
  wait text       Wait for text or a regex to appear

Common flags:
  --name <name>     Target a named daemon (default: TWEE_SESSION or "default")
  --timeout <dur>   Override timeout for wait verbs

Only long options are accepted. Global --name may appear before
name-aware daemon commands, for example "twee --name foo status".

Use "--" before child argv or literal payloads:
  twee start -- vim file
  twee run --script ops.json -- ./myapp
  twee type -- literal text

For literal characters use "twee type --"; "twee key" only accepts named
keys (Enter, Down, Ctrl+C, ...). "twee key i" fails — use "twee type -- i".

Output is JSON by default:
  {"ok": true, "data": {...}}            on success
  {"ok": false, "error": {...}}          on failure

Run "twee help <verb>" for per-verb usage (e.g. "twee help start",
"twee help wait text").`)
}

type commandSummary struct {
	name    string
	summary string
}

var commandSummaries = []commandSummary{
	{"cell", "Show one cell at x,y"},
	{"codegen", "Interactively author a run script"},
	{"completion", "Print shell completion setup"},
	{"cursor", "Show cursor state"},
	{"diff", "Compare the viewport to a saved text snapshot"},
	{"find", "Find text in the viewport"},
	{"help", "Print top-level or per-command help"},
	{"key", "Send one named key"},
	{"keys", "Send multiple named keys"},
	{"lines", "Show visible viewport lines"},
	{"ls", "List running daemons"},
	{"mode", "Show active terminal modes"},
	{"paste", "Send bracketed paste text"},
	{"play", "Play a .twee trace bundle"},
	{"record", "Start or stop JSONL recording"},
	{"region", "Show cells in a rectangular region"},
	{"resize", "Resize the terminal"},
	{"run", "Run a one-shot ephemeral session"},
	{"screenshot", "Render the current screen to PNG"},
	{"scrollback", "Show retained scrollback"},
	{"signal", "Send a signal to the child process"},
	{"size", "Show terminal dimensions"},
	{"sleep", "Sleep client-side"},
	{"snapshot", "Show the full terminal snapshot"},
	{"start", "Spawn a TUI in a daemon"},
	{"status", "Show daemon status"},
	{"stop", "Stop the running daemon"},
	{"text", "Show visible viewport text"},
	{"title", "Show the window title"},
	{"trace", "Start or stop .twee trace recording"},
	{"type", "Write literal text to the PTY"},
	{"version", "Print the twee version"},
	{"wait", "Wait for terminal state or process exit"},
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
