package daemon

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
	tinput "github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/rpc"
)

func TestInputHandlers(t *testing.T) {
	te := startTestTerm(t)

	tests := []struct {
		name string
		fn   Handler
		args any
	}{
		{"type", handleType, rpc.TypeArgs{Text: "abc"}},
		{"key", handleKey, rpc.KeyArgs{Key: "Enter"}},
		{"paste", handlePaste, rpc.PasteArgs{Text: "pasted", Force: true}},
		{"signal", handleSignal, rpc.SignalArgs{Name: "WINCH"}},
		{"resize", handleResize, rpc.ResizeArgs{Cols: 50, Rows: 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(te, mustJSON(t, tt.args)); err != nil {
				t.Fatalf("%s: %+v", tt.name, err)
			}
		})
	}

	snap := te.Snapshot()
	if snap.Cols != 50 || snap.Rows != 7 {
		t.Fatalf("snapshot size = %dx%d, want 50x7", snap.Cols, snap.Rows)
	}
}

func TestInputHandlersRejectInvalidArgs(t *testing.T) {
	te := startTestTerm(t)

	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
	}{
		{"type json", handleType, json.RawMessage(`{`)},
		{"key json", handleKey, json.RawMessage(`{`)},
		{"key unknown", handleKey, mustJSON(t, rpc.KeyArgs{Key: "Nope"})},
		{"paste json", handlePaste, json.RawMessage(`{`)},
		{"signal json", handleSignal, json.RawMessage(`{`)},
		{"signal unknown", handleSignal, mustJSON(t, rpc.SignalArgs{Name: "SIGNOPE"})},
		{"resize json", handleResize, json.RawMessage(`{`)},
		{"resize invalid", handleResize, mustJSON(t, rpc.ResizeArgs{Cols: 0, Rows: 5})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(te, tt.raw); err == nil {
				t.Fatalf("%s unexpectedly succeeded", tt.name)
			} else if err.Code != rpc.CodeInvalidArgument {
				t.Fatalf("error code = %q, want %q", err.Code, rpc.CodeInvalidArgument)
			}
		})
	}
}

func TestKeyWriteFailureIsIO(t *testing.T) {
	te := startTestTerm(t)
	if err := te.Close(); err != nil {
		t.Fatal(err)
	}
	_, errResp := handleKey(te, mustJSON(t, rpc.KeyArgs{Key: "Tab"}))
	if errResp == nil || errResp.Code != rpc.CodeIO {
		t.Fatalf("key after PTY close error = %+v, want IO", errResp)
	}
}

func TestPasteHandlerRequiresEnabledModeUnlessForced(t *testing.T) {
	te := startTestTerm(t)
	_, errResp := handlePaste(te, mustJSON(t, rpc.PasteArgs{Text: "pasted"}))
	if errResp == nil {
		t.Fatal("paste unexpectedly succeeded with mode 2004 disabled")
	}
	if errResp.Code != rpc.CodeFailedPrecondition {
		t.Fatalf("error code = %q, want %q", errResp.Code, rpc.CodeFailedPrecondition)
	}
	if _, errResp := handlePaste(te, mustJSON(t, rpc.PasteArgs{Text: "pasted", Force: true})); errResp != nil {
		t.Fatalf("forced paste: %+v", errResp)
	}
}

func TestParseSignalAliases(t *testing.T) {
	for _, name := range []string{"SIGTERM", "TERM", "SIGKILL", "KILL", "SIGINT", "INT", "SIGHUP", "HUP", "SIGWINCH", "WINCH", "SIGUSR1", "USR1", "SIGUSR2", "USR2"} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSignal(name); err != nil {
				t.Fatalf("parseSignal(%q): %v", name, err)
			}
		})
	}
	if _, err := parseSignal("SIGBOGUS"); err == nil {
		t.Fatal("parseSignal(SIGBOGUS) unexpectedly succeeded")
	}
}

func TestMouseHandlersRejectMissingAndInvalidArguments(t *testing.T) {
	te := startTestTerm(t)
	x, y := 2, 1
	fromX, fromY, toX := 1, 1, 3

	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
	}{
		{"click missing x", handleClick, mustJSON(t, rpc.ClickArgs{Y: &y})},
		{"click null y", handleClick, json.RawMessage(`{"x":2,"y":null}`)},
		{"click unknown field", handleClick, json.RawMessage(`{"x":2,"y":1,"z":3}`)},
		{"click invalid button", handleClick, mustJSON(t, rpc.ClickArgs{X: &x, Y: &y, Button: "primary"})},
		{"click unknown modifier", handleClick, mustJSON(t, rpc.ClickArgs{X: &x, Y: &y, Modifiers: []string{"meta"}})},
		{"click duplicate modifier", handleClick, mustJSON(t, rpc.ClickArgs{X: &x, Y: &y, Modifiers: []string{"ctrl", "ctrl"}})},
		{"hover missing y", handleHover, mustJSON(t, rpc.HoverArgs{X: &x})},
		{"scroll missing x", handleScroll, mustJSON(t, rpc.ScrollArgs{Y: &y, Direction: "down"})},
		{"scroll invalid direction", handleScroll, mustJSON(t, rpc.ScrollArgs{X: &x, Y: &y, Direction: "left"})},
		{"drag missing to y", handleDrag, mustJSON(t, rpc.DragArgs{
			FromX: &fromX, FromY: &fromY, ToX: &toX,
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, errResp := tt.fn(te, tt.raw); errResp == nil {
				t.Fatal("handler unexpectedly succeeded")
			} else if errResp.Code != rpc.CodeInvalidArgument {
				t.Fatalf("error code = %q, want %q", errResp.Code, rpc.CodeInvalidArgument)
			}
		})
	}
}

func TestMouseHandlersReportDisabledTrackingAsFailedPrecondition(t *testing.T) {
	te := startTestTerm(t)
	x, y := 2, 1
	_, errResp := handleClick(te, mustJSON(t, rpc.ClickArgs{X: &x, Y: &y}))
	if errResp == nil {
		t.Fatal("click unexpectedly succeeded with mouse tracking disabled")
	}
	if errResp.Code != rpc.CodeFailedPrecondition {
		t.Fatalf("error code = %q, want %q", errResp.Code, rpc.CodeFailedPrecondition)
	}
}

func TestMouseHandlers(t *testing.T) {
	te := startMouseTestTerm(t, "1003")
	x, y := 2, 1
	fromX, fromY, toX, toY := 1, 1, 3, 2
	ticks := 3

	tests := []struct {
		name string
		fn   Handler
		args any
	}{
		{"click", handleClick, rpc.ClickArgs{X: &x, Y: &y}},
		{"hover", handleHover, rpc.HoverArgs{X: &x, Y: &y, Modifiers: []string{"ctrl"}}},
		{"scroll", handleScroll, rpc.ScrollArgs{X: &x, Y: &y, Direction: "down", Ticks: &ticks}},
		{"drag", handleDrag, rpc.DragArgs{
			FromX: &fromX, FromY: &fromY, ToX: &toX, ToY: &toY,
			Button: "right",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, errResp := tt.fn(te, mustJSON(t, tt.args)); errResp != nil {
				t.Fatalf("handler error = %+v", errResp)
			}
		})
	}

	inputs := te.RecentInputs()
	if got, want := len(inputs), len(tests); got != want {
		t.Fatalf("diagnostic inputs = %d, want %d: %#v", got, want, inputs)
	}
}

func TestFindClickHandlerSelectionAndErrors(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf '\\033[?1003h\\033[?1006hSubmit  界界  Submit'; sleep 30"},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("Submit  界界  Submit"); err != nil {
		t.Fatal(err)
	}

	_, rpcErr := handleFindClick(te, mustJSON(t, rpc.FindClickArgs{Pattern: "missing"}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeNotFound {
		t.Fatalf("missing error = %+v", rpcErr)
	}
	_, rpcErr = handleFindClick(te, mustJSON(t, rpc.FindClickArgs{Pattern: "Submit"}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeAmbiguousMatch {
		t.Fatalf("ambiguous error = %+v", rpcErr)
	}
	var details struct {
		Pattern    string          `json:"pattern"`
		Regex      bool            `json:"regex"`
		MatchCount int             `json:"match_count"`
		Selection  string          `json:"selection"`
		Matches    []rpc.FindMatch `json:"matches"`
	}
	if err := json.Unmarshal(rpcErr.Details, &details); err != nil || details.Pattern != "Submit" || details.Regex ||
		details.MatchCount != 2 || details.Selection != "exactly_one" || len(details.Matches) != 2 {
		t.Fatalf("ambiguous details = %+v, %v", details, err)
	}
	_, rpcErr = handleFindClick(te, mustJSON(t, rpc.FindClickArgs{Pattern: "Submit", Select: new("3")}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeInvalidSelection {
		t.Fatalf("selection error = %+v", rpcErr)
	}
	_, rpcErr = handleFindClick(te, mustJSON(t, rpc.FindClickArgs{Pattern: "Submit", Select: new("one")}))
	if rpcErr == nil || rpcErr.Code != rpc.CodeInvalidSelection {
		t.Fatalf("named selection error = %+v", rpcErr)
	}

	data, rpcErr := handleFindClick(te, mustJSON(t, rpc.FindClickArgs{
		Pattern: "界+", Regex: true, Select: new("first"), Button: "right", Modifiers: []string{"ctrl"},
	}))
	if rpcErr != nil {
		t.Fatalf("find click: %+v", rpcErr)
	}
	got := data.(rpc.FindClickData)
	if got.Match.Text != "界界" || got.Match.W != 4 || got.Target.X != got.Match.X+1 || got.Selection != "first" {
		t.Fatalf("data = %+v", got)
	}
}

func TestFindClickHandlerStrictArgs(t *testing.T) {
	te := startTestTerm(t)
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"pattern":"x","selection":"first"}`),
		mustJSON(t, rpc.FindClickArgs{Pattern: "x", Require: new("any")}),
		mustJSON(t, rpc.FindClickArgs{Pattern: "x", Require: new("one"), Select: new("first")}),
		mustJSON(t, rpc.FindClickArgs{Pattern: "x", Require: new("")}),
		mustJSON(t, rpc.FindClickArgs{Pattern: "x", Select: new("")}),
		mustJSON(t, rpc.FindClickArgs{Pattern: "x", Require: new(""), Select: new("")}),
	} {
		if _, rpcErr := handleFindClick(te, raw); rpcErr == nil || rpcErr.Code != rpc.CodeInvalidArgument {
			t.Fatalf("strict args error = %+v for %s", rpcErr, raw)
		}
	}
}

func TestFindClickRejectsZeroWidthRegexTargets(t *testing.T) {
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", "printf '\\033[?1003h\\033[?1006hABCDEFGH'; sleep 30"},
		Cols: 8, Rows: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("ABCDEFGH"); err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"", "^", "$"} {
		_, rpcErr := handleFindClick(te, mustJSON(t, rpc.FindClickArgs{
			Pattern: pattern, Regex: true, Select: new("first"),
		}))
		if rpcErr == nil || rpcErr.Code != rpc.CodeInvalidSelection {
			t.Fatalf("pattern %q error = %+v", pattern, rpcErr)
		}
		var details struct {
			Pattern   string        `json:"pattern"`
			Regex     bool          `json:"regex"`
			Selection string        `json:"selection"`
			Match     rpc.FindMatch `json:"match"`
		}
		if err := json.Unmarshal(rpcErr.Details, &details); err != nil || details.Pattern != pattern || !details.Regex ||
			details.Selection != "first" || details.Match.W != 0 {
			t.Fatalf("pattern %q details = %+v, %v", pattern, details, err)
		}
		if pattern == "$" && details.Match.X != 8 {
			t.Fatalf("right-edge match = %+v, want x=8 without click", details.Match)
		}
	}
	if got := len(te.RecentInputs()); got != 0 {
		t.Fatalf("zero-width matches recorded %d clicks", got)
	}
}

func TestMouseHandlerCoordinateDetails(t *testing.T) {
	te := startMouseTestTerm(t, "1003")
	x, y := 40, 1
	_, errResp := handleClick(te, mustJSON(t, rpc.ClickArgs{X: &x, Y: &y}))
	if errResp == nil {
		t.Fatal("out-of-bounds click unexpectedly succeeded")
	}
	if errResp.Code != rpc.CodeInvalidArgument {
		t.Fatalf("error code = %q, want %q", errResp.Code, rpc.CodeInvalidArgument)
	}
	var details struct {
		X, Y       int
		Cols, Rows int
	}
	if err := json.Unmarshal(errResp.Details, &details); err != nil {
		t.Fatalf("details: %v", err)
	}
	if details.X != x || details.Y != y || details.Cols != 40 || details.Rows != 5 {
		t.Fatalf("details = %+v, want x=%d y=%d cols=40 rows=5", details, x, y)
	}
	if got := len(te.RecentInputs()); got != 0 {
		t.Fatalf("failed click recorded %d diagnostics, want 0", got)
	}
}

func TestMouseHandlerRejectsInvalidTicksBeforeProtocolCheck(t *testing.T) {
	te := startTestTerm(t)
	x, y := 2, 1
	for _, ticks := range []int{0, -1, tinput.MaxScrollTicks + 1} {
		t.Run(strconv.Itoa(ticks), func(t *testing.T) {
			_, errResp := handleScroll(te, mustJSON(t, rpc.ScrollArgs{
				X: &x, Y: &y, Direction: "down", Ticks: &ticks,
			}))
			if errResp == nil || errResp.Code != rpc.CodeInvalidArgument {
				t.Fatalf("ticks %d error = %+v, want INVALID_ARGUMENT", ticks, errResp)
			}
		})
	}
}

func startMouseTestTerm(t *testing.T, trackingMode string) *engine.Term {
	t.Helper()
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd: []string{
			"/bin/sh", "-c",
			"printf '\\033[?" + trackingMode + "h\\033[?1006hREADY'; sleep 30",
		},
		Cols: 40,
		Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("READY"); err != nil {
		t.Fatalf("WaitForText READY: %v", err)
	}
	return te
}
