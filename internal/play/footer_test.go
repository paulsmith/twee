package play

import (
	"bytes"
	"testing"
)

func TestWriteFooter(t *testing.T) {
	tests := []struct {
		name                      string
		terminalCols, frameCols   int
		rows                      int
		toast, status, wantOutput string
	}{
		{
			name:         "uses terminal width",
			terminalCols: 4,
			frameCols:    8,
			rows:         3,
			toast:        "toast",
			status:       "ok\x1b",
			wantOutput:   "\x1b[4;1H\x1b[2Ktoa…\x1b[5;1H\x1b[2Kok \x1b[H",
		},
		{
			name:         "falls back to frame width",
			terminalCols: 0,
			frameCols:    3,
			rows:         1,
			toast:        "abcd",
			status:       "\x01x",
			wantOutput:   "\x1b[2;1H\x1b[2Kab…\x1b[3;1H\x1b[2K x\x1b[H",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeFooter(&out, tt.terminalCols, tt.frameCols, tt.rows, tt.toast, tt.status); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tt.wantOutput {
				t.Fatalf("output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}
