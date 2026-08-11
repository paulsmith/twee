package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func intPointer(v int) *int          { return &v }
func boolPointer(v bool) *bool       { return &v }
func stringPointer(v string) *string { return &v }
func bytePointer(v uint8) *uint8     { return &v }

func TestPredicateHandlersWaitAndAssert(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "sleep 0.05; printf '\033[31;1mX'; sleep 30"},
		Cols: 10, Rows: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.Close() })

	red := &rpc.ColorPredicate{Kind: rpc.ColorKindPalette, Index: bytePointer(1)}
	predicate := rpc.CellPredicate{Text: stringPointer("X"), Fg: red, Bold: boolPointer(true)}
	if _, rpcErr := handleWaitCell(te, mustJSON(t, rpc.WaitCellArgs{
		X: intPointer(0), Y: intPointer(0), Predicate: predicate, Timeout: "1s",
	})); rpcErr != nil {
		t.Fatalf("wait cell: %+v", rpcErr)
	}
	if _, rpcErr := handleAssertCell(te, mustJSON(t, rpc.AssertCellArgs{
		X: intPointer(0), Y: intPointer(0), Predicate: predicate,
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
		X: intPointer(0), Y: intPointer(0), Predicate: rpc.CellPredicate{Bold: boolPointer(true)},
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
		X: intPointer(100), Y: intPointer(0), W: intPointer(1), H: intPointer(1),
		Predicate: rpc.CellPredicate{Text: stringPointer("x")},
	}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeAssertionFailed {
		t.Fatalf("off-screen assert region error = %+v, want %s", rpcErr, rpc.CodeAssertionFailed)
	}
}

func TestWaitCellTimeoutIncludesPredicateAndActualCell(t *testing.T) {
	te := startTestTerm(t)
	_, rpcErr := handleWaitCell(te, mustJSON(t, rpc.WaitCellArgs{
		X: intPointer(0), Y: intPointer(0), Timeout: "1ms",
		Predicate: rpc.CellPredicate{Bold: boolPointer(true)},
	}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeTimeout {
		t.Fatalf("wait cell error = %+v, want %s", rpcErr, rpc.CodeTimeout)
	}
	var details map[string]any
	if err := json.Unmarshal(rpcErr.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["actual"] == nil || details["predicate"] == nil || details["last_screen"] == nil {
		t.Fatalf("wait details = %v, want actual, predicate, and last_screen", details)
	}
}

func TestPredicateHandlersRejectInvalidArguments(t *testing.T) {
	te := startTestTerm(t)
	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
	}{
		{"missing coordinate", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{Y: intPointer(0), Predicate: rpc.CellPredicate{Bold: boolPointer(true)}})},
		{"empty predicate", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: intPointer(0), Y: intPointer(0)})},
		{"bad width", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: intPointer(0), Y: intPointer(0), Predicate: rpc.CellPredicate{Width: intPointer(3)}})},
		{"incomplete RGB", handleAssertCell, mustJSON(t, rpc.AssertCellArgs{X: intPointer(0), Y: intPointer(0), Predicate: rpc.CellPredicate{Fg: &rpc.ColorPredicate{Kind: rpc.ColorKindRGB, R: bytePointer(1)}}})},
		{"partial region", handleAssertRegion, mustJSON(t, rpc.AssertRegionArgs{X: intPointer(0), Predicate: rpc.CellPredicate{Bold: boolPointer(true)}})},
		{"bad match", handleAssertRegion, mustJSON(t, rpc.AssertRegionArgs{Match: "some", Predicate: rpc.CellPredicate{Bold: boolPointer(true)}})},
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
