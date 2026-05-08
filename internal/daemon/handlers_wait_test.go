package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
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
		{"no text json", handleWaitNoText, json.RawMessage(`{`), rpc.CodeInvalidArgument},
		{"no text timeout", handleWaitNoText, mustJSON(t, rpc.WaitNoTextArgs{Text: "hello", Timeout: "nope"}), rpc.CodeInvalidArgument},
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
