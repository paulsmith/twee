package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/paulsmith/twee/internal/inspect"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func init() {
	register("inspect", runInspect)
	registerUsage("inspect", `twee inspect [--format json|text] <bundle.twee>
Inspect a .twee trace bundle and print summary metadata.

Flags:
  --format json|text    output format (default json)`)
}

func runInspect(args []string) {
	var parsed struct {
		Format string `arg:"--format"`
		Path   string `arg:"positional,required"`
	}
	if err := parseArg("inspect", &parsed, args); err != nil {
		fatalUsage("inspect: %v", err)
	}
	format := parsed.Format
	if format == "" {
		format = "json"
	}
	switch format {
	case "json", "text":
	default:
		fatalUsage("inspect: invalid --format %q (want json or text)", format)
	}

	bundle, err := tracebundle.Open(parsed.Path)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	summary := inspect.Summarize(parsed.Path, bundle)
	if format == "json" {
		emitOK(summary)
		return
	}
	printInspectText(os.Stdout, summary)
}

func printInspectText(w io.Writer, s inspect.Summary) {
	fmt.Fprintf(w, "Path: %s\n", s.Path)
	fmt.Fprintf(w, "Version: %d\n", s.Version)
	fmt.Fprintf(w, "Command: %s\n", formatCommand(s.Command))
	fmt.Fprintf(w, "Duration: %s (%d ms)\n", s.Duration, s.DurationMS)
	fmt.Fprintf(w, "Event span: %d ms\n", s.EventSpanMS)
	fmt.Fprintf(w, "Terminal: %dx%d (max %dx%d)\n", s.Terminal.Cols, s.Terminal.Rows, s.Terminal.MaxCols, s.Terminal.MaxRows)
	fmt.Fprintf(w, "Events: %d total\n", s.Events.Total)
	fmt.Fprintf(w, "Types: %s\n", formatCounts(s.Events.ByType))
	fmt.Fprintf(w, "Input: %s\n", formatCounts(s.Events.InputByKind))
	if s.Exit.Recorded && s.Exit.Code != nil {
		fmt.Fprintf(w, "Exit: code %d\n", *s.Exit.Code)
		return
	}
	fmt.Fprintln(w, "Exit: not recorded")
}

func formatCommand(command []string) string {
	if len(command) == 0 {
		return "(none)"
	}
	return strings.Join(command, " ")
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}
