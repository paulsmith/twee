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
		{"paste", handlePaste, rpc.PasteArgs{Text: "pasted"}},
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
