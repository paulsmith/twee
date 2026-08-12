package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/paulsmith/twee/internal/inspect"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func init() {
	register("inspect", runInspect)
	registerUsage("inspect", `twee inspect [--format json|text] <bundle.twee>
Validate a .twee trace bundle, replay output and resize events through the
terminal model, and print metadata plus final semantic state. Replay includes
final dimensions, visible text, cursor, alternate screen, styled cells, modes,
and control-sequence-granular mode transitions. Validation checks zip integrity,
the manifest and version, every event record, timestamp order, replay-safe
terminal dimensions, child PTY termios captured at trace start and child exit,
and any declared network capture. Invalid bundles report every validation problem in error.details.issues.

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
	if output.machine && format == "text" {
		fatalUsage("inspect: --format text is not compatible with --machine")
	}

	decoded, validation, err := tracebundle.OpenValidated(parsed.Path)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	if !validation.Valid {
		emitInvalidBundle("inspect", validation.Issues)
	}

	replay, err := inspect.Replay(decoded)
	if err != nil {
		var limitErr *inspect.LimitError
		if errors.As(err, &limitErr) {
			emitInvalidBundle("inspect", []string{limitErr.Error()})
		}
		emitError(rpc.CodeInternal, err.Error(), nil, 1)
	}
	summary := inspect.Summarize(parsed.Path, decoded)
	summary.Replay = replay
	if format == "json" {
		emitOK(summary)
		return
	}
	printInspectText(os.Stdout, summary)
}

func emitInvalidBundle(operation string, issues []string) {
	details, _ := json.Marshal(map[string]any{"issues": issues})
	emitError(rpc.CodeInvalidArgument,
		fmt.Sprintf("%s: %d issue(s) found", operation, len(issues)),
		details, 1)
}

func printInspectText(w io.Writer, s inspect.Summary) {
	_, _ = fmt.Fprintf(w, "Path: %s\n", s.Path)
	_, _ = fmt.Fprintf(w, "Version: %d\n", s.Version)
	_, _ = fmt.Fprintf(w, "Command: %s\n", formatCommand(s.Command))
	_, _ = fmt.Fprintf(w, "Duration: %s (%d ms)\n", s.Duration, s.DurationMS)
	_, _ = fmt.Fprintf(w, "Event span: %d ms\n", s.EventSpanMS)
	_, _ = fmt.Fprintf(w, "Terminal: %dx%d (max %dx%d)\n", s.Terminal.Cols, s.Terminal.Rows, s.Terminal.MaxCols, s.Terminal.MaxRows)
	_, _ = fmt.Fprintf(w, "Events: %d total\n", s.Events.Total)
	_, _ = fmt.Fprintf(w, "Types: %s\n", formatCounts(s.Events.ByType))
	_, _ = fmt.Fprintf(w, "Input: %s\n", formatCounts(s.Events.InputByKind))
	if s.Exit.Recorded && s.Exit.Code != nil {
		_, _ = fmt.Fprintf(w, "Exit: code %d\n", *s.Exit.Code)
	} else {
		_, _ = fmt.Fprintln(w, "Exit: not recorded")
	}
	printChildPTYTermiosText(w, s.ChildPTYTermios)
	if !s.Network.Present {
		_, _ = fmt.Fprintln(w, "Network capture: none")
	} else {
		_, _ = fmt.Fprintf(w, "Network capture: %s, %d packets, %d bytes, status %s", s.Network.Format, s.Network.PacketCount, s.Network.SizeBytes, s.Network.Status)
		if s.Network.Truncated {
			_, _ = fmt.Fprint(w, " (truncated)")
		}
		_, _ = fmt.Fprintln(w)
	}
	printInspectReplayText(w, s.Replay)
}

func printChildPTYTermiosText(w io.Writer, summary inspect.ChildPTYTermiosSummary) {
	if !summary.Present {
		_, _ = fmt.Fprintln(w, "Child PTY termios: not recorded")
		return
	}
	_, _ = fmt.Fprintf(w, "Child PTY termios (%s): start %s; exit %s\n",
		summary.Platform, formatTermiosSnapshot(summary.Start), formatTermiosSnapshot(summary.Exit))
}

func formatTermiosSnapshot(snapshot *termios.Snapshot) string {
	if snapshot == nil {
		return "not recorded"
	}
	if snapshot.Status != termios.StatusCaptured || snapshot.State == nil {
		if snapshot.Error != "" {
			return fmt.Sprintf("%s (%s)", snapshot.Status, snapshot.Error)
		}
		return snapshot.Status
	}
	state := snapshot.State
	return fmt.Sprintf("canonical=%t echo=%t signals=%t", state.Canonical, state.Echo, state.Signals)
}

func printInspectReplayText(w io.Writer, replay inspect.ReplaySummary) {
	final := replay.Final
	if final.EventIndex == nil || final.TMS == nil {
		_, _ = fmt.Fprintf(w, "Replay final: %dx%d (initial state)\n", final.Size.Cols, final.Size.Rows)
	} else {
		_, _ = fmt.Fprintf(w, "Replay final: %dx%d at %d ms (event %d)\n", final.Size.Cols, final.Size.Rows, *final.TMS, *final.EventIndex)
	}
	_, _ = fmt.Fprintf(w, "Cursor: x=%d y=%d visible=%t shape=%s\n", final.Cursor.X, final.Cursor.Y, final.Cursor.Visible, final.Cursor.Shape)
	_, _ = fmt.Fprintf(w, "Alternate screen: %t\n", final.AltScreen)
	_, _ = fmt.Fprintf(w, "Modes: decckm=%t, application_keypad=%t, bracketed_paste=%t, focus_events=%t, kitty_keyboard_known=%t, kitty_keyboard_flags=%d, mouse=%t, mouse_known=%t, mouse_raw=%t\n",
		final.Modes.DECCKM, final.Modes.ApplicationKeypad, final.Modes.BracketedPaste,
		final.Modes.FocusEvents, final.Modes.KittyKeyboardKnown, final.Modes.KittyKeyboardFlags,
		final.Modes.Mouse, final.Modes.MouseKnown, final.Modes.MouseRaw)
	_, _ = fmt.Fprintf(w, "Mode transitions: %d\n", len(replay.ModeTransitions))
	for _, transition := range replay.ModeTransitions {
		_, _ = fmt.Fprintf(w, "  %d ms event=%d byte=%d: %s\n", transition.TMS, transition.EventIndex, transition.ByteOffset, strings.Join(transition.Changed, ", "))
	}
	_, _ = fmt.Fprintf(w, "Styled cells: %d\n", final.StyledCells)
	_, _ = fmt.Fprintln(w, "Final viewport:")
	for row, line := range strings.Split(final.VisibleText, "\n") {
		_, _ = fmt.Fprintf(w, "%4d | %s\n", row, line)
	}
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
