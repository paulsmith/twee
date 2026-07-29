package play

import "testing"

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatEventToast(tt.ev); got != tt.want {
				t.Fatalf("toast = %q, want %q", got, tt.want)
			}
		})
	}
}
