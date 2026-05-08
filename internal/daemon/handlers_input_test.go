package daemon

import (
	"encoding/json"
	"testing"

	"github.com/paulsmith/research/twee/internal/rpc"
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
