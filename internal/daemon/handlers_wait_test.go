package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func TestWaitHandlers(t *testing.T) {
	te := startTestTerm(t)
	c := te.CursorPos()

	tests := []struct {
		name string
		fn   Handler
		args any
	}{
		{"text", handleWaitText, rpc.WaitTextArgs{Text: "hello", Timeout: "1s"}},
		{"text regex", handleWaitText, rpc.WaitTextArgs{Text: `h.llo`, Regex: true, Timeout: "1s"}},
		{"no text", handleWaitNoText, rpc.WaitNoTextArgs{Text: "missing", Timeout: "1s"}},
		{"stable", handleWaitStable, rpc.WaitStableArgs{Quiet: "1ms", Timeout: "1s"}},
		{"cursor", handleWaitCursor, rpc.WaitCursorArgs{X: c.Col, Y: c.Row, Timeout: "1s"}},
		{"sleep", handleSleep, rpc.SleepArgs{Duration: "1ns"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(te, mustJSON(t, tt.args)); err != nil {
				t.Fatalf("%s: %+v", tt.name, err)
			}
		})
	}
}

func TestWaitStableHandlerRejectsInvalidExclude(t *testing.T) {
	te := startTestTerm(t)
	for _, rect := range []rpc.Rect{
		{X: -1, Y: 0, W: 1, H: 1},
		{X: 0, Y: -1, W: 1, H: 1},
		{X: 0, Y: 0, W: 0, H: 1},
		{X: 0, Y: 0, W: 1, H: 0},
	} {
		_, errResp := handleWaitStable(te, mustJSON(t, rpc.WaitStableArgs{
			Quiet: "1ms", Timeout: "1s", Exclude: []rpc.Rect{rect},
		}))
		if errResp == nil {
			t.Fatalf("exclude %+v unexpectedly succeeded", rect)
		}
		if errResp.Code != rpc.CodeInvalidArgument {
			t.Fatalf("exclude %+v error code = %q, want %q", rect, errResp.Code, rpc.CodeInvalidArgument)
		}
	}
}

// TestWaitTextRegexAnchorsPerLine pins down that --regex patterns are
// compiled in multi-line mode: "^bravo" must match a "bravo" line even
// though it isn't the first line of the viewport. Without (?m), Go's
// default ^/$ only anchor at the start/end of the whole joined
// viewport string, so this would time out instead.
func TestWaitTextRegexAnchorsPerLine(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf 'alpha\\r\\nbravo\\r\\n'; sleep 30"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("bravo"); err != nil {
		t.Fatalf("WaitForText(bravo): %v", err)
	}

	if _, errResp := handleWaitText(te, mustJSON(t, rpc.WaitTextArgs{
		Text: "^bravo", Regex: true, Timeout: "1s",
	})); errResp != nil {
		t.Fatalf("wait text --regex '^bravo': %+v", errResp)
	}
	if _, errResp := handleWaitText(te, mustJSON(t, rpc.WaitTextArgs{
		Text: "bravo$", Regex: true, Timeout: "1s",
	})); errResp != nil {
		t.Fatalf("wait text --regex 'bravo$': %+v", errResp)
	}
}

func TestWaitExitHandler(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "exit 7"},
		Cols: 10,
		Rows: 3,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })

	data, rpcErr := handleWaitExit(te, mustJSON(t, rpc.WaitExitArgs{Timeout: "2s"}))
	if rpcErr != nil {
		t.Fatalf("handleWaitExit: %+v", rpcErr)
	}
	if got := data.(rpc.WaitExitData).ExitCode; got != 7 {
		t.Fatalf("exit code = %d, want 7", got)
	}
}

func TestWaitHandlersRejectInvalidArgs(t *testing.T) {
	te := startTestTerm(t)

	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
		code string
	}{
		{"text json", handleWaitText, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"text timeout", handleWaitText, mustJSON(t, rpc.WaitTextArgs{Text: "hello", Timeout: "nope"}), rpc.CodeInvalidArgument},
		{"text regex", handleWaitText, mustJSON(t, rpc.WaitTextArgs{Text: `(`, Regex: true}), rpc.CodeInvalidArgument},
		{"text missing", handleWaitText, mustJSON(t, rpc.WaitTextArgs{Text: "missing", Timeout: "1ms"}), rpc.CodeTimeout},
		{"text empty no regex", handleWaitText, mustJSON(t, rpc.WaitTextArgs{Text: "", Timeout: "1s"}), rpc.CodeInvalidArgument},
		{"text unknown key", handleWaitText, json.RawMessage(`{"pattern":"never"}`), rpc.CodeInvalidArgument},
		{"no text json", handleWaitNoText, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"no text timeout", handleWaitNoText, mustJSON(t, rpc.WaitNoTextArgs{Text: "hello", Timeout: "nope"}), rpc.CodeInvalidArgument},
		{"no text empty", handleWaitNoText, mustJSON(t, rpc.WaitNoTextArgs{Text: "", Timeout: "1s"}), rpc.CodeInvalidArgument},
		{"stable json", handleWaitStable, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"stable quiet", handleWaitStable, mustJSON(t, rpc.WaitStableArgs{Quiet: "nope"}), rpc.CodeInvalidArgument},
		{"stable timeout", handleWaitStable, mustJSON(t, rpc.WaitStableArgs{Timeout: "nope"}), rpc.CodeInvalidArgument},
		{"cursor json", handleWaitCursor, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"cursor timeout", handleWaitCursor, mustJSON(t, rpc.WaitCursorArgs{Timeout: "nope"}), rpc.CodeInvalidArgument},
		{"exit json", handleWaitExit, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"exit timeout", handleWaitExit, mustJSON(t, rpc.WaitExitArgs{Timeout: "nope"}), rpc.CodeInvalidArgument},
		{"sleep json", handleSleep, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"sleep duration", handleSleep, mustJSON(t, rpc.SleepArgs{Duration: "nope"}), rpc.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(te, tt.raw); err == nil {
				t.Fatalf("%s unexpectedly succeeded", tt.name)
			} else if err.Code != tt.code {
				t.Fatalf("error code = %q, want %q", err.Code, tt.code)
			}
		})
	}
}

// TestWaitTextEmptyRegexAllowed documents the boundary of the "text or
// regex required" check added alongside strict arg decoding: it rejects
// empty text only in literal mode (the footgun where a misnamed key left
// Text at its zero value and matched instantly). An explicit
// {"regex":true,"text":""} is a deliberate, if odd, "match anything"
// pattern and is left alone.
func TestWaitTextEmptyRegexAllowed(t *testing.T) {
	te := startTestTerm(t)
	if _, errResp := handleWaitText(te, mustJSON(t, rpc.WaitTextArgs{
		Text: "", Regex: true, Timeout: "1s",
	})); errResp != nil {
		t.Fatalf("handleWaitText(regex, empty text): %+v", errResp)
	}
}

// TestWaitTimeoutDetailsCauseIsShort pins down that details.cause is the
// short root cause of a timed-out wait, not a second copy of the
// multi-line diagnostic dump that already lives in message.
func TestWaitTimeoutDetailsCauseIsShort(t *testing.T) {
	te := startTestTerm(t)

	_, errResp := handleWaitText(te, mustJSON(t, rpc.WaitTextArgs{
		Text: "text that never appears", Timeout: "50ms",
	}))
	if errResp == nil {
		t.Fatal("handleWaitText unexpectedly succeeded")
	}
	if errResp.Code != rpc.CodeTimeout {
		t.Fatalf("code = %q, want %q", errResp.Code, rpc.CodeTimeout)
	}
	if !strings.Contains(errResp.Message, "--- visible screen ---") {
		t.Fatalf("message missing diagnostic dump: %q", errResp.Message)
	}
	var details struct {
		Cause      string `json:"cause"`
		LastScreen string `json:"last_screen"`
	}
	if err := json.Unmarshal(errResp.Details, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if details.Cause != "pump: timeout" {
		t.Fatalf("details.cause = %q, want %q", details.Cause, "pump: timeout")
	}
	if strings.Contains(details.Cause, "--- visible screen ---") {
		t.Fatalf("details.cause duplicates the diagnostic dump: %q", details.Cause)
	}
	if details.LastScreen == "" {
		t.Fatal("details.last_screen unexpectedly empty")
	}
}

// TestWaitSessionEndedOnPumpClose pins down that wait_text, wait_no_text,
// wait_cursor, and wait_cell report SESSION_ENDED rather than TIMEOUT when the pump
// closes (the child exits) before the wait's target state is reached and
// before its deadline fires — so scripts can tell "the app is slow" from
// "the app is gone" without string-matching cause. Each case uses a child
// that prints "hello" and exits almost immediately, then waits on a
// condition ("never appears" / "hello" never disappears / a cursor
// position the child never reaches / a cell value never painted) that can only
// resolve via the pump closing, well before the 5s timeout.
func TestWaitSessionEndedOnPumpClose(t *testing.T) {
	spawn := func(t *testing.T) *engine.Term {
		t.Helper()
		te, err := engine.Start(context.Background(), engine.Config{
			Cmd:  []string{"/bin/sh", "-c", "printf 'hello\\r\\n'; sleep 0.05"},
			Cols: 40, Rows: 5,
		})
		if err != nil {
			t.Fatalf("engine.Start: %v", err)
		}
		t.Cleanup(func() { _ = te.Close() })
		if err := te.WaitForText("hello"); err != nil {
			t.Fatalf("WaitForText(hello): %v", err)
		}
		return te
	}

	tests := []struct {
		name string
		call func(te *engine.Term) (any, *rpc.Error)
	}{
		{"text", func(te *engine.Term) (any, *rpc.Error) {
			return handleWaitText(te, mustJSON(t, rpc.WaitTextArgs{Text: "never appears", Timeout: "5s"}))
		}},
		{"no text", func(te *engine.Term) (any, *rpc.Error) {
			return handleWaitNoText(te, mustJSON(t, rpc.WaitNoTextArgs{Text: "hello", Timeout: "5s"}))
		}},
		{"cursor", func(te *engine.Term) (any, *rpc.Error) {
			return handleWaitCursor(te, mustJSON(t, rpc.WaitCursorArgs{X: 39, Y: 4, Timeout: "5s"}))
		}},
		{"cell", func(te *engine.Term) (any, *rpc.Error) {
			return handleWaitCell(te, mustJSON(t, rpc.WaitCellArgs{
				X: intPointer(0), Y: intPointer(0), Timeout: "5s",
				Predicate: rpc.CellPredicate{Text: stringPointer("never")},
			}))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := spawn(t)
			_, errResp := tt.call(te)
			if errResp == nil {
				t.Fatal("wait unexpectedly succeeded")
			}
			if errResp.Code != rpc.CodeSessionEnded {
				t.Fatalf("code = %q, want %q", errResp.Code, rpc.CodeSessionEnded)
			}
			var details struct {
				Cause      string `json:"cause"`
				LastScreen string `json:"last_screen"`
			}
			if err := json.Unmarshal(errResp.Details, &details); err != nil {
				t.Fatalf("decode details: %v", err)
			}
			if details.Cause != "pump: closed" {
				t.Fatalf("details.cause = %q, want %q", details.Cause, "pump: closed")
			}
			if details.LastScreen == "" {
				t.Fatal("details.last_screen unexpectedly empty")
			}
		})
	}
}

// TestWaitStableStaysSuccessOnPumpClose documents the deliberate
// exception: unlike text/no-text/cursor, wait_stable does NOT report
// SESSION_ENDED when the pump closes (see engine.IsSessionEnded's doc
// comment). A child that exits well within the quiet window still
// reports success, matching the pre-SESSION_ENDED behavior — flipping
// this would break the "closed is trivially stable" shortcut that real
// apps exiting right after their last paint rely on.
func TestWaitStableStaysSuccessOnPumpClose(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf 'hi\\r\\n'"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })

	_, errResp := handleWaitStable(te, mustJSON(t, rpc.WaitStableArgs{Quiet: "50ms", Timeout: "2s"}))
	if errResp != nil {
		t.Fatalf("handleWaitStable: %+v", errResp)
	}
}

// TestWaitExitTimeoutIncludesDiagnostic verifies that wait exit now carries the
// same bounded failure context as the other waits while retaining a short cause.
func TestWaitExitTimeoutIncludesDiagnostic(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "sleep 30"},
		Cols: 10, Rows: 3,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })

	_, errResp := handleWaitExit(te, mustJSON(t, rpc.WaitExitArgs{Timeout: "50ms"}))
	if errResp == nil {
		t.Fatal("handleWaitExit unexpectedly succeeded")
	}
	var details struct {
		Cause      string         `json:"cause"`
		LastScreen string         `json:"last_screen"`
		Cursor     rpc.CursorData `json:"cursor"`
		Modes      rpc.ModeData   `json:"modes"`
	}
	if err := json.Unmarshal(errResp.Details, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if details.Cause != "WaitForExit: timeout after 50ms" {
		t.Fatalf("wait exit cause = %q", details.Cause)
	}
	if !strings.Contains(errResp.Message, "--- visible screen ---") {
		t.Fatalf("message missing diagnostic: %q", errResp.Message)
	}
	if details.Cursor.X != 0 || details.Cursor.Y != 0 || details.Modes.AltScreen {
		t.Fatalf("diagnostic cursor/modes = %+v / %+v", details.Cursor, details.Modes)
	}
}

func TestParseTimeout(t *testing.T) {
	fallback := 5 * time.Second
	if got, err := parseTimeout("", fallback); err != nil || got != fallback {
		t.Fatalf("parseTimeout empty = %v, %v; want %v, nil", got, err, fallback)
	}
	if got, err := parseTimeout("10ms", fallback); err != nil || got != 10*time.Millisecond {
		t.Fatalf("parseTimeout 10ms = %v, %v; want 10ms, nil", got, err)
	}
	if _, err := parseTimeout("bad", fallback); err == nil {
		t.Fatal("parseTimeout bad unexpectedly succeeded")
	}
}
