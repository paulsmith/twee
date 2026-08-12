// Command twee drives terminal UIs from shell scripts and tests.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func init() {
	register("completion", runCompletion)
	register("help", runHelp)
	register("version", runVersion)
	registerUsage("completion", `twee completion <bash|zsh|fish>
Print a shell completion script. Completion generation is currently a
placeholder.`)
	registerUsage("help", `twee help [<verb> [<subverb>...]] [--format text|json]
Print top-level help or per-command usage. JSON output is a versioned command
descriptor API for automation.`)
	registerUsage("version", `twee version
Print the twee version.`)
}

func main() {
	if inDaemonMode() {
		runDaemonChild()
		return // unreachable
	}
	args, machine, err := extractMachineMode(os.Args[1:])
	output.machine = machine
	if err != nil {
		fatalUsage("%v", err)
	}
	root, err := parseRootArgs(args)
	if err != nil {
		fatalUsage("%v", err)
	}
	rootGlobalName = root.GlobalName
	if root.Help {
		if output.machine {
			var text bytes.Buffer
			if root.HelpKey == "" {
				printUsage(&text)
			} else {
				printVerbHelp(&text, strings.Split(root.HelpKey, " "))
			}
			emitTextSuccess(strings.TrimSuffix(text.String(), "\n"))
			return
		}
		if root.HelpKey == "" {
			printUsage(os.Stdout)
			return
		}
		printVerbHelp(os.Stdout, strings.Split(root.HelpKey, " "))
		return
	}
	if root.Verb == "" {
		if output.machine {
			fatalUsage("missing subcommand")
		}
		printUsage(os.Stderr)
		os.Exit(2)
	}
	verb := root.Verb
	verbArgs := root.Args

	if d := commandRegistry[verb]; d != nil && d.handler != nil {
		if output.machine && d.Interactive {
			fatalUsage("%s: --machine is not supported for interactive commands", verb)
		}
		d.handler(verbArgs)
		return
	}
	if output.machine {
		fatalUsage("unknown subcommand %q", verb)
	}
	fmt.Fprintf(os.Stderr, "twee: unknown subcommand %q\n", verb)
	printUsage(os.Stderr)
	os.Exit(2)
}

func printVerbHelp(w io.Writer, args []string) {
	key := strings.Join(args, " ")
	if d := commandRegistry[key]; d != nil && d.Usage != "" {
		_, _ = fmt.Fprintln(w, d.Usage)
		return
	}
	// Fall back to the first word — e.g. "help wait" prints the wait
	// overview when no specific subverb was given.
	if d := commandRegistry[args[0]]; d != nil && d.Usage != "" {
		_, _ = fmt.Fprintln(w, d.Usage)
		return
	}
	fatalUsage("no help available for %q", key)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `twee - drive TUIs from the shell.

Usage: twee [--machine] [--name <session>] <verb> [args...]

Commands:`)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range sortedDescriptors() {
		if len(cmd.Path) == 1 {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", cmd.Path[0], cmd.Summary)
		}
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(w, `
Wait commands:
  wait cell       Wait for a physical cell predicate
  wait cursor     Wait for the cursor to reach a position
  wait exit       Wait for the child process to exit
  wait no-text    Wait for text to disappear
  wait stable     Wait for the screen to stop changing
  wait text       Wait for text or a regex to appear

Common flags:
  --machine         emit structured success and error output; place before the command
  --name <name>     Target a named daemon (default: TWEE_SESSION or "default")
  --timeout <dur>   Override timeout for wait verbs

Most commands accept only long options. Global --name may appear before
name-aware daemon commands, for example "twee --name foo status".
The export command also accepts -o for its output path.

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
"twee help wait text"). Use "twee help --format json" to discover the
versioned output contract for every command.`)
}

func runHelp(args []string) {
	format := "text"
	var path []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--format":
			if i+1 >= len(args) {
				fatalUsage("help: --format requires a value")
			}
			format, i = args[i+1], i+1
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		case strings.HasPrefix(args[i], "-"):
			fatalUsage("help: unknown option %s", args[i])
		default:
			path = append(path, args[i])
		}
	}
	if format != "text" && format != "json" {
		fatalUsage("help: invalid --format %q (want json or text)", format)
	}
	if format == "json" {
		if output.machine {
			value, err := jsonHelpValue(path)
			if err != nil {
				fatalUsage("help: %v", err)
			}
			emitOK(value)
			return
		}
		if err := writeJSONHelp(os.Stdout, path); err != nil {
			fatalUsage("help: %v", err)
		}
		return
	}
	if output.machine {
		var text bytes.Buffer
		if len(path) > 0 {
			printVerbHelp(&text, path)
		} else {
			printUsage(&text)
		}
		emitTextSuccess(strings.TrimSuffix(text.String(), "\n"))
		return
	}
	if len(path) > 0 {
		printVerbHelp(os.Stdout, path)
		return
	}
	printUsage(os.Stdout)
}

func runCompletion(args []string) {
	if len(args) == 0 {
		fatalUsage("completion: missing shell argument (bash|zsh|fish)")
	}
	switch args[0] {
	case "bash", "zsh", "fish":
		emitTextSuccess("# twee completion: not yet generated")
	default:
		fatalUsage("completion: unknown shell %q", args[0])
	}
}

func runVersion(_ []string) { emitTextSuccess(Version) }

func inDaemonMode() bool { return inDaemonModeReal() }
func runDaemonChild()    { runDaemonChildReal() }
