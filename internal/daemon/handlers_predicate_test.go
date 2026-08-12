package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func TestPredicateHandlersWaitAndAssert(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "sleep 0.05; printf '\033[31;1mX'; sleep 30"},
		Cols: 10, Rows: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.Close() })

	red := &rpc.ColorPredicate{Kind: rpc.ColorKindPalette, Index: new(uint8(1))}
	predicate := rpc.CellPredicate{Text: new("X"), Fg: red, Bold: new(true)}
	if _, rpcErr := handleWaitCell(te, mustJSON(t, rpc.WaitCellArgs{
		X: new(0), Y: new(0), Predicate: predicate, Timeout: "1s",
	})); rpcErr != nil {
		t.Fatalf("wait cell: %+v", rpcErr)
	}
	if _, rpcErr := handleAssertCell(te, mustJSON(t, rpc.AssertCellArgs{
		X: new(0), Y: new(0), Predicate: predicate,
	})); rpcErr != nil {
		t.Fatalf("assert cell: %+v", rpcErr)
	}
	if _, rpcErr := handleAssertRegion(te, mustJSON(t, rpc.AssertRegionArgs{
		Match: "any", Predicate: rpc.CellPredicate{Fg: red},
	})); rpcErr != nil {
		t.Fatalf("assert region: %+v", rpcErr)
	}
}

func TestAssertPredicateMismatchHasDedicatedCodeAndActualCell(t *testing.T) {
	te := startTestTerm(t)
	_, rpcErr := handleAssertCell(te, mustJSON(t, rpc.AssertCellArgs{
		X: new(0), Y: new(0), Predicate: rpc.CellPredicate{Bold: new(true)},
	}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeAssertionFailed {
		t.Fatalf("assert cell error = %+v, want %s", rpcErr, rpc.CodeAssertionFailed)
	}
	var details map[string]any
	if err := json.Unmarshal(rpcErr.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["actual"] == nil || details["predicate"] == nil {
		t.Fatalf("assertion details = %v, want actual and predicate", details)
	}

	_, rpcErr = handleAssertRegion(te, mustJSON(t, rpc.AssertRegionArgs{
		X: new(100), Y: new(0), W: new(1), H: new(1),
		Predicate: rpc.CellPredicate{Text: new("x")},
	}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeAssertionFailed {
		t.Fatalf("off-screen assert region error = %+v, want %s", rpcErr, rpc.CodeAssertionFailed)
	}
	var regionDetails struct {
		Summary struct {
			TotalCells        int            `json:"total_cells"`
			MatchingCells     int            `json:"matching_cells"`
			EmptyIntersection bool           `json:"empty_intersection"`
			Clipped           map[string]int `json:"clipped"`
		} `json:"region_summary"`
		Cursor rpc.CursorData `json:"cursor"`
		Modes  rpc.ModeData   `json:"modes"`
	}
	if err := json.Unmarshal(rpcErr.Details, &regionDetails); err != nil {
		t.Fatal(err)
	}
	if regionDetails.Summary.TotalCells != 0 || !regionDetails.Summary.EmptyIntersection || regionDetails.Summary.Clipped != nil {
		t.Fatalf("region summary = %+v", regionDetails.Summary)
	}
}

func TestWaitCellTimeoutIncludesPredicateAndActualCell(t *testing.T) {
	te := startTestTerm(t)
	if err := te.Resize(20, 4); err != nil {
		t.Fatal(err)
	}
	if err := te.Type("diagnostic input"); err != nil {
		t.Fatal(err)
	}
	_, rpcErr := handleWaitCell(te, mustJSON(t, rpc.WaitCellArgs{
		X: new(0), Y: new(0), Timeout: "1ms",
		Predicate: rpc.CellPredicate{Bold: new(true)},
	}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeTimeout {
		t.Fatalf("wait cell error = %+v, want %s", rpcErr, rpc.CodeTimeout)
	}
	var details struct {
		Actual       any              `json:"actual"`
		Predicate    any              `json:"predicate"`
		LastScreen   string           `json:"last_screen"`
		CapturedAt   string           `json:"captured_at"`
		Cursor       rpc.CursorData   `json:"cursor"`
		Modes        rpc.ModeData     `json:"modes"`
		RecentEvents []map[string]any `json:"recent_events"`
	}
	if err := json.Unmarshal(rpcErr.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Actual == nil || details.Predicate == nil || details.LastScreen == "" || details.CapturedAt == "" {
		t.Fatalf("wait details = %+v, want actual, predicate, screen, and capture time", details)
	}
	if details.Cursor.X < 0 || details.Modes.AltScreen {
		t.Fatalf("cursor/modes = %+v / %+v", details.Cursor, details.Modes)
	}
	seen := map[string]bool{}
	for _, event := range details.RecentEvents {
		seen[event["type"].(string)] = true
		if description, _ := event["description"].(string); strings.Contains(description, "diagnostic input") {
			t.Fatalf("structured recent event exposed typed payload: %v", event)
		}
	}
	for _, kind := range []string{"resize", "input", "output"} {
		if !seen[kind] {
			t.Fatalf("recent events = %v, missing %q", details.RecentEvents, kind)
		}
	}
}

func TestAssertionFailureIncludesActiveTrace(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "failure.twee")
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: []string{"/bin/sh", "-c", "printf X; sleep 30"}, Cols: 10, Rows: 2,
		WholeSessionTrace: &engine.WholeSessionTraceConfig{Path: tracePath},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("X"); err != nil {
		t.Fatal(err)
	}
	_, rpcErr := handleAssertCell(te, mustJSON(t, rpc.AssertCellArgs{
		X: new(0), Y: new(0), Predicate: rpc.CellPredicate{Text: new("Y")},
	}))
	if rpcErr == nil {
		t.Fatal("assertion unexpectedly succeeded")
	}
	var details struct {
		Trace struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"trace"`
	}
	if err := json.Unmarshal(rpcErr.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Trace.Path != tracePath || details.Trace.Status != "active" {
		t.Fatalf("trace = %+v", details.Trace)
	}
}

func TestPredicateHandlersRejectInvalidArguments(t *testing.T) {
	te := startTestTerm(t)
	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
	}{
		{"missing coordinate", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{Y: new(0), Predicate: rpc.CellPredicate{Bold: new(true)}})},
		{"empty predicate", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: new(0), Y: new(0)})},
		{"bad width", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: new(0), Y: new(0), Predicate: rpc.CellPredicate{Width: new(3)}})},
		{"incomplete RGB", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: new(0), Y: new(0), Predicate: rpc.CellPredicate{Fg: &rpc.ColorPredicate{Kind: rpc.ColorKindRGB, R: new(uint8(1))}}})},
		{"partial region", handleAssertRegion, mustJSON(t, rpc.AssertRegionArgs{X: new(0), Predicate: rpc.CellPredicate{Bold: new(true)}})},
		{"bad match", handleAssertRegion, mustJSON(t, rpc.AssertRegionArgs{Match: "some", Predicate: rpc.CellPredicate{Bold: new(true)}})},
		{"nested unknown", handleAssertCell, json.RawMessage(`{"x":0,"y":0,"predicate":{"blink":true}}`)},
		{"duplicate predicate", handleAssertCell, json.RawMessage(`{"x":0,"y":0,"predicate":{"text":"bad","text":""}}`)},
		{"noncanonical predicate", handleAssertCell, json.RawMessage(`{"x":0,"y":0,"predicate":{"TEXT":"x"}}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, rpcErr := test.fn(te, test.raw); rpcErr == nil || rpcErr.Code != rpc.CodeInvalidArgument {
				t.Fatalf("error = %+v, want %s", rpcErr, rpc.CodeInvalidArgument)
			}
		})
	}
}
