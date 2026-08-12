package play

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteStatusRow(t *testing.T) {
	var out bytes.Buffer
	if err := writeStatusRow(&out, 24, 8, "toast", "status"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"\x1b[8;1H\x1b[0m\x1b[2K\x1b[7m",
		"status │ toast",
		"\x1b[0m\x1b[H",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q in %q", want, got)
		}
	}
}

func TestWriteStatusRowListsSpeedControls(t *testing.T) {
	var out bytes.Buffer
	if err := writeStatusRow(&out, 200, 8, "", "status"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "-/+ speed") {
		t.Fatalf("footer = %q, want speed controls", got)
	}
}

func TestWriteStatusRowSanitizesTruncatesAndFillsWidth(t *testing.T) {
	var out bytes.Buffer
	if err := writeStatusRow(&out, 8, 3, "", "abc\x1b[2Jdef"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("status contains raw escape: %q", got)
	}
	if !strings.Contains(got, "\x1b[7mabc [2J…\x1b[0m") {
		t.Fatalf("sanitized status missing from %q", got)
	}
}

func TestWriteStatusRowUsesDisplayCellWidth(t *testing.T) {
	tests := []struct {
		name, status, want string
		cols               int
	}{
		{
			name:   "double width",
			cols:   6,
			status: "界界界",
			want:   "\x1b[3;1H\x1b[0m\x1b[2K\x1b[7m界界… \x1b[0m\x1b[H",
		},
		{
			name:   "combining mark",
			cols:   5,
			status: "e\u0301界界",
			want:   "\x1b[3;1H\x1b[0m\x1b[2K\x1b[7me\u0301界… \x1b[0m\x1b[H",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeStatusRow(&out, tt.cols, 3, "", tt.status); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClearStatusRow(t *testing.T) {
	var out bytes.Buffer
	if err := clearStatusRow(&out, 12); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\x1b[12;1H\x1b[0m\x1b[2K\x1b[H"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
