package play

import (
	"testing"

	"github.com/paulsmith/twee/internal/trace"
)

func TestFormatEventToast(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		want string
	}{
		{
			name: "key",
			ev:   Event{TMS: 2314, Type: "input", Kind: "key", Key: "Enter"},
			want: "[02.314s] \u2192 Enter",
		},
		{
			name: "type",
			ev:   Event{TMS: 2871, Type: "input", Kind: "type", Bytes: []byte("hello")},
			want: "[02.871s] \u2192 type \"hello\"",
		},
		{
			name: "paste strips bracketed markers",
			ev:   Event{TMS: 3105, Type: "input", Kind: "paste", Bytes: []byte("\x1b[200~hello\nworld\x1b[201~")},
			want: "[03.105s] \u2192 paste \"hello\\nworld\"",
		},
		{
			name: "resize",
			ev:   Event{TMS: 3105, Type: "resize", Cols: 100, Rows: 40},
			want: "[03.105s] \u2192 resize 100x40",
		},
		{
			name: "mouse click includes zero coordinate",
			ev: Event{
				TMS: 1250, Type: "input", Kind: "mouse",
				Mouse: &trace.MouseInput{
					Gesture: "click", X: intPtr(0), Y: intPtr(4), Button: "left",
					Modifiers: []string{},
				},
			},
			want: "[01.250s] \u2192 click left @(0,4)",
		},
		{
			name: "mouse hover",
			ev: Event{
				TMS: 2100, Type: "input", Kind: "mouse",
				Mouse: &trace.MouseInput{
					Gesture: "hover", X: intPtr(20), Y: intPtr(8),
					Modifiers: []string{"ctrl"},
				},
			},
			want: "[02.100s] \u2192 hover @(20,8)",
		},
		{
			name: "mouse scroll",
			ev: Event{
				TMS: 2100, Type: "input", Kind: "mouse",
				Mouse: &trace.MouseInput{
					Gesture: "scroll", X: intPtr(20), Y: intPtr(8),
					Direction: "down", Ticks: 3, Modifiers: []string{},
				},
			},
			want: "[02.100s] \u2192 scroll down x3 @(20,8)",
		},
		{
			name: "mouse drag",
			ev: Event{
				TMS: 3000, Type: "input", Kind: "mouse",
				Mouse: &trace.MouseInput{
					Gesture: "drag", FromX: intPtr(4), FromY: intPtr(2),
					ToX: intPtr(30), ToY: intPtr(12), Button: "left",
					Modifiers: []string{},
				},
			},
			want: "[03.000s] \u2192 drag left (4,2)->(30,12)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatEventToast(tt.ev); got != tt.want {
				t.Fatalf("toast = %q, want %q", got, tt.want)
			}
		})
	}
}

func intPtr(v int) *int { return &v }
