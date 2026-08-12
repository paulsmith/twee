package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"
)

const commandSchemaVersion = 1

type artifactDescriptor struct {
	PathField string `json:"path_field,omitempty"`
}

type ndjsonDescriptor struct {
	Record          string `json:"record"`
	TerminalFailure string `json:"terminal_failure"`
	FinalSummary    bool   `json:"final_summary"`
}

type commandDescriptor struct {
	Path             []string            `json:"path"`
	Summary          string              `json:"summary,omitempty"`
	Usage            string              `json:"usage"`
	Interactive      bool                `json:"interactive"`
	SuccessOutput    string              `json:"success_output"`
	Formats          []string            `json:"formats"`
	StructuredErrors bool                `json:"structured_errors"`
	ExitStatus       map[string]string   `json:"exit_status"`
	Artifact         *artifactDescriptor `json:"artifact,omitempty"`
	NDJSON           *ndjsonDescriptor   `json:"ndjson,omitempty"`
	handler          func([]string)
}

type helpDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	Commands      []commandDescriptor `json:"commands"`
}

type commandHelpDocument struct {
	SchemaVersion int               `json:"schema_version"`
	Command       commandDescriptor `json:"command"`
}

var commandRegistry = newCommandRegistry()

func newCommandRegistry() map[string]*commandDescriptor {
	summaries := map[string]string{
		"assert": "Assert terminal cell or region state", "cell": "Show one cell at x,y", "click": "Click a viewport cell or visible match",
		"completion": "Print shell completion setup", "cursor": "Show cursor state",
		"diff": "Compare the viewport to a saved text snapshot", "do": "Run an op script against a running session",
		"drag": "Drag between viewport cells", "export": "Export a .twee trace bundle to GIF, self-contained HTML, MP4, or WebM",
		"find": "Find text in the viewport", "help": "Print top-level or per-command help",
		"hover": "Move the mouse to a viewport cell", "inspect": "Validate and inspect a .twee trace bundle",
		"key": "Send one named key", "keys": "Send multiple named keys", "lines": "Show visible viewport lines",
		"ls": "List running daemons", "mode": "Show active terminal modes", "paste": "Send bracketed paste text",
		"play": "Play a .twee trace bundle", "region": "Show cells in a rectangular region",
		"resize": "Resize the terminal", "run": "Run a one-shot ephemeral session",
		"screenshot": "Render the current screen to PNG", "scroll": "Send vertical wheel input",
		"scrollback": "Show retained scrollback", "signal": "Send a signal to the child process",
		"size": "Show terminal dimensions", "sleep": "Sleep client-side", "snapshot": "Show the full terminal snapshot",
		"start": "Spawn a TUI in a daemon", "status": "Show daemon status", "stop": "Stop the running daemon",
		"text": "Show visible viewport text", "title": "Show the window title", "trace": "Start, stop, or query .twee traces",
		"type": "Write literal text to the PTY", "version": "Print the twee version",
		"wait": "Wait for terminal state or process exit", "wrap": "Wrap a terminal command with optional recording",
	}
	registry := make(map[string]*commandDescriptor, len(summaries))
	for name, summary := range summaries {
		kind, formats, structured := "json", []string{"json"}, true
		interactive := name == "play" || name == "wrap"
		if interactive {
			kind, formats, structured = "interactive", []string{}, false
		}
		switch name {
		case "completion", "help", "version":
			kind, formats = "text", []string{"text", "json"}
		case "export":
			kind, formats = "silent", []string{"json"}
		case "run", "do":
			formats = []string{"json", "ndjson"}
		case "inspect":
			formats = []string{"json", "text"}
		}
		d := &commandDescriptor{
			Path: []string{name}, Summary: summary, Interactive: interactive,
			SuccessOutput: kind, Formats: formats, StructuredErrors: structured,
			ExitStatus: map[string]string{"0": "success", "1": "operational failure", "2": "usage failure"},
		}
		if name == "diff" {
			d.ExitStatus["0"] = "comparison completed; inspect data.equal"
		}
		switch name {
		case "export":
			d.Artifact = &artifactDescriptor{PathField: "data.path"}
		case "screenshot":
			d.Artifact = &artifactDescriptor{PathField: "data.out"}
		case "start":
			d.Artifact = &artifactDescriptor{PathField: "data.trace"}
		case "run", "trace", "wrap":
			d.Artifact = &artifactDescriptor{}
		}
		if name == "run" || name == "do" {
			d.NDJSON = &ndjsonDescriptor{
				Record:          "one response per completed operation",
				TerminalFailure: "the failing response is the terminal record; exit status 1",
				FinalSummary:    false,
			}
		}
		registry[name] = d
	}
	return registry
}

func descriptorKey(path []string) string { return strings.Join(path, " ") }

func register(verb string, fn func(args []string)) {
	d, ok := commandRegistry[verb]
	if !ok {
		panic("missing command descriptor for " + verb)
	}
	d.handler = fn
}

func registerUsage(path, usage string) {
	d, ok := commandRegistry[path]
	if !ok {
		parts := strings.Split(path, " ")
		parent := commandRegistry[parts[0]]
		if parent == nil {
			panic("missing parent command descriptor for " + path)
		}
		copy := *parent
		copy.Path, copy.Summary, copy.handler = parts, "", nil
		d = &copy
		commandRegistry[path] = d
	}
	d.Usage = usage
	switch path {
	case "trace start":
		d.Artifact = &artifactDescriptor{PathField: "data.out"}
	case "trace mark":
		d.Artifact = nil
	case "trace stop":
		d.Artifact = &artifactDescriptor{PathField: "data.path"}
	case "trace contains-output":
		d.Artifact = nil
		exitStatus := make(map[string]string, len(d.ExitStatus))
		maps.Copy(exitStatus, d.ExitStatus)
		exitStatus["1"] = "no output match (ASSERTION_FAILED), invalid bundle, or operational failure"
		d.ExitStatus = exitStatus
	}
}

func sortedDescriptors() []commandDescriptor {
	commands := make([]commandDescriptor, 0, len(commandRegistry))
	for _, d := range commandRegistry {
		commands = append(commands, *d)
	}
	sort.Slice(commands, func(i, j int) bool { return descriptorKey(commands[i].Path) < descriptorKey(commands[j].Path) })
	return commands
}

func writeJSONHelp(w io.Writer, path []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	value, err := jsonHelpValue(path)
	if err != nil {
		return err
	}
	return enc.Encode(value)
}

func jsonHelpValue(path []string) (any, error) {
	if len(path) == 0 {
		return helpDocument{SchemaVersion: commandSchemaVersion, Commands: sortedDescriptors()}, nil
	}
	d := commandRegistry[descriptorKey(path)]
	if d == nil {
		return nil, fmt.Errorf("no help available for %q", descriptorKey(path))
	}
	return commandHelpDocument{SchemaVersion: commandSchemaVersion, Command: *d}, nil
}
