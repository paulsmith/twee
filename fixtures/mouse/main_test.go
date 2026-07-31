package main

import (
	"strings"
	"testing"
)

func TestReportParserHandlesSplitAndCoalescedSGR(t *testing.T) {
	p := newReportParser("sgr")
	if got, err := p.feed([]byte("\x1b[<0;1")); err != nil || len(got) != 0 {
		t.Fatalf("partial feed = %v, %v", got, err)
	}
	got, err := p.feed([]byte(";1M\x1b[<0;1;1m"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"EVENT action=press button=left x=0 y=0 modifiers=",
		"EVENT action=release button=none x=0 y=0 modifiers=",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("reports = %#v, want %#v", got, want)
	}
}

func TestReportParserProtocols(t *testing.T) {
	tests := []struct {
		name   string
		format string
		report string
		want   string
	}{
		{
			name: "x10", format: "x10",
			report: "\x1b[M" + string([]byte{32, 33 + 12, 33 + 4}),
			want:   "EVENT action=press button=left x=12 y=4 modifiers=",
		},
		{
			name: "utf8 large", format: "utf8",
			report: "\x1b[M \u014d\u01b1",
			want:   "EVENT action=press button=left x=300 y=400 modifiers=",
		},
		{
			name: "sgr hover", format: "sgr",
			report: "\x1b[<35;21;9M",
			want:   "EVENT action=motion button=none x=20 y=8 modifiers=",
		},
		{
			name: "urxvt release", format: "urxvt",
			report: "\x1b[35;3;4M",
			want:   "EVENT action=release button=none x=2 y=3 modifiers=",
		},
		{
			name: "modifiers", format: "sgr",
			report: "\x1b[<28;3;4M",
			want:   "EVENT action=press button=left x=2 y=3 modifiers=shift,alt,ctrl",
		},
		{
			name: "wheel down", format: "sgr",
			report: "\x1b[<65;1;1M",
			want:   "EVENT action=press button=wheel_down x=0 y=0 modifiers=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newReportParser(tt.format)
			got, err := p.feed([]byte(tt.report))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("reports = %#v, want %q", got, tt.want)
			}
		})
	}
}

func TestModeSequences(t *testing.T) {
	on, off, err := modeSequences(config{tracking: "any", format: "sgr"})
	if err != nil {
		t.Fatal(err)
	}
	if on != "\x1b[?1003h\x1b[?1006h" || off != "\x1b[?1003l\x1b[?1006l" {
		t.Fatalf("sequences = %q, %q", on, off)
	}
	if _, _, err := modeSequences(config{tracking: "bad", format: "sgr"}); err == nil {
		t.Fatal("unknown tracking accepted")
	}
	if _, _, err := modeSequences(config{tracking: "any", format: "bad"}); err == nil {
		t.Fatal("unknown format accepted")
	}
}
