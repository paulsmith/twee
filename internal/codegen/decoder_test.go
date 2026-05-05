package codegen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/paulsmith/research/twee/internal/rpc"
)

func TestDecodeBytes(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []inputEvent
	}{
		{
			name: "printable coalesced",
			in:   []byte("hello 世界"),
			want: []inputEvent{{kind: inputType, text: "hello 世界", bytes: []byte("hello 世界")}},
		},
		{
			name: "known keys",
			in:   []byte("\x1b[A\r\x7f"),
			want: []inputEvent{
				{kind: inputKey, key: "Up", bytes: []byte("\x1b[A")},
				{kind: inputKey, key: "Enter", bytes: []byte("\r")},
				{kind: inputKey, key: "Backspace", bytes: []byte{0x7f}},
			},
		},
		{
			name: "ctrl key and recorder prefix",
			in:   []byte{0x03, controlPrefix, 'q'},
			want: []inputEvent{
				{kind: inputKey, key: "Ctrl+C", bytes: []byte{0x03}},
				{kind: inputControl, control: 'q'},
			},
		},
		{
			name: "bracketed paste",
			in:   []byte("\x1b[200~hello\nworld\x1b[201~"),
			want: []inputEvent{{kind: inputPaste, text: "hello\nworld", bytes: []byte("\x1b[200~hello\nworld\x1b[201~")}},
		},
		{
			name: "unknown escape is forwarded but not semantic",
			in:   []byte("\x1b[9~"),
			want: []inputEvent{{kind: inputUnknown, bytes: []byte("\x1b[9~")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeBytes(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d: %#v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].kind != tt.want[i].kind || got[i].text != tt.want[i].text ||
					got[i].key != tt.want[i].key || got[i].control != tt.want[i].control ||
					string(got[i].bytes) != string(tt.want[i].bytes) {
					t.Fatalf("event %d = %#v, want %#v", i, got[i], tt.want[i])
				}
				if got[i].kind == inputUnknown && got[i].warning == "" {
					t.Fatalf("event %d missing warning", i)
				}
			}
		})
	}
}

func TestDecoderPreservesSplitControlPrefix(t *testing.T) {
	var dec Decoder
	if got := dec.Decode([]byte{controlPrefix}); len(got) != 0 {
		t.Fatalf("first decode = %#v, want no events", got)
	}
	got := dec.Decode([]byte("q"))
	if len(got) != 1 || got[0].kind != inputControl || got[0].control != 'q' {
		t.Fatalf("second decode = %#v, want Ctrl+] q", got)
	}
}

func TestDecoderHandlesTraceToggleControl(t *testing.T) {
	got := DecodeBytes([]byte{controlPrefix, 't'})
	if len(got) != 1 || got[0].kind != inputControl || got[0].control != 't' {
		t.Fatalf("decode = %#v, want Ctrl+] t", got)
	}
}

func TestDecoderPreservesSplitEscapeSequence(t *testing.T) {
	var dec Decoder
	if got := dec.Decode([]byte("\x1b[")); len(got) != 0 {
		t.Fatalf("first decode = %#v, want no events", got)
	}
	got := dec.Decode([]byte("A"))
	if len(got) != 1 || got[0].kind != inputKey || got[0].key != "Up" {
		t.Fatalf("second decode = %#v, want Up", got)
	}
}

func TestDecoderPreservesSplitPaste(t *testing.T) {
	var dec Decoder
	if got := dec.Decode([]byte("\x1b[200~hello")); len(got) != 0 {
		t.Fatalf("first decode = %#v, want no events", got)
	}
	got := dec.Decode([]byte("\nworld\x1b[201~"))
	if len(got) != 1 || got[0].kind != inputPaste || got[0].text != "hello\nworld" {
		t.Fatalf("second decode = %#v, want paste", got)
	}
}

func TestDecoderPreservesSplitUTF8(t *testing.T) {
	var dec Decoder
	if got := dec.Decode([]byte{0xe4}); len(got) != 0 {
		t.Fatalf("first decode = %#v, want no events", got)
	}
	got := dec.Decode([]byte{0xb8, 0x96})
	if len(got) != 1 || got[0].kind != inputType || got[0].text != "世" {
		t.Fatalf("second decode = %#v, want 世", got)
	}
}

func TestDecoderFlushesPendingEscape(t *testing.T) {
	var dec Decoder
	if got := dec.Decode([]byte{0x1b}); len(got) != 0 {
		t.Fatalf("decode = %#v, want no events", got)
	}
	got := dec.Flush()
	if len(got) != 1 || got[0].kind != inputKey || got[0].key != "Escape" {
		t.Fatalf("flush = %#v, want Escape", got)
	}
}

func TestRecorderEmitsRunScriptOps(t *testing.T) {
	var rec recorder
	if err := rec.Type("a"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Type("b"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Key("Enter"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Paste("x\ny"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	if err := rec.WaitStable(); err != nil {
		t.Fatal(err)
	}

	got := rec.Requests()
	if len(got) != 5 {
		t.Fatalf("ops = %d, want 5: %#v", len(got), got)
	}
	if got[0].Op != rpc.OpType || string(got[0].Args) != `{"text":"ab"}` {
		t.Fatalf("first op = %s %s", got[0].Op, got[0].Args)
	}
	assertOp(t, got[1], rpc.OpKey, `{"key":"Enter"}`)
	assertOp(t, got[2], rpc.OpPaste, "{\"text\":\"x\\ny\"}")
	assertOp(t, got[3], rpc.OpResize, `{"cols":100,"rows":40}`)
	assertOp(t, got[4], rpc.OpWaitStable, `{}`)
}

func TestWarningSummaryReportsOnce(t *testing.T) {
	var warnings warningSummary
	warnings.Add("unknown escape sequence omitted from script: 1b 5b 39 7e")
	warnings.Add("unknown input byte omitted from script: 80")

	var out bytes.Buffer
	warnings.Report(&out)
	got := out.String()
	if !strings.Contains(got, "omitted 2 unknown input sequences") {
		t.Fatalf("summary = %q", got)
	}
	if strings.Count(got, "twee codegen:") != 1 {
		t.Fatalf("summary reported multiple lines: %q", got)
	}
}

func assertOp(t *testing.T, got any, op, args string) {
	t.Helper()
	req := got.(rpc.Request)
	if req.Op != op || string(req.Args) != args {
		t.Fatalf("op = %s %s, want %s %s", req.Op, req.Args, op, args)
	}
}
