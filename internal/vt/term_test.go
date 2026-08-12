package vt

import (
	"strings"
	"testing"
)

func feed(t *testing.T, cols, rows int, in string) Snapshot {
	t.Helper()
	m := New(cols, rows)
	if err := m.Feed([]byte(in)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	return m.Snapshot()
}

func TestPlainText(t *testing.T) {
	s := feed(t, 20, 3, "hello")
	got := VisibleLines(s)
	if got[0] != "hello" {
		t.Errorf("line 0 = %q, want %q", got[0], "hello")
	}
	if s.Cursor.Col != 5 || s.Cursor.Row != 0 {
		t.Errorf("cursor = (%d,%d), want (5,0)", s.Cursor.Col, s.Cursor.Row)
	}
}

func TestCRLF(t *testing.T) {
	s := feed(t, 20, 3, "a\r\nb\r\nc")
	got := VisibleLines(s)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBackspace(t *testing.T) {
	s := feed(t, 10, 1, "abc\b\bX")
	if got := VisibleLines(s)[0]; got != "aXc" {
		t.Errorf("got %q, want %q", got, "aXc")
	}
}

func TestCursorMovement(t *testing.T) {
	// Move to (3,1) (1-based), write "X". CSI H is 1;1.
	s := feed(t, 10, 3, "abc\x1b[2;3HX")
	lines := VisibleLines(s)
	if lines[0] != "abc" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "  X" {
		t.Errorf("line 1 = %q, want %q", lines[1], "  X")
	}
}

func TestEraseDisplay(t *testing.T) {
	// Fill, then ED 2.
	s := feed(t, 5, 2, "abcde\r\nfghij\x1b[2J")
	for i, ln := range VisibleLines(s) {
		if ln != "" {
			t.Errorf("line %d = %q, want empty", i, ln)
		}
	}
}

func TestEraseLine(t *testing.T) {
	// Fill, then move to col 3, EL 0 (cursor to end).
	s := feed(t, 5, 1, "abcde\x1b[1;3H\x1b[0K")
	if got := VisibleLines(s)[0]; got != "ab" {
		t.Errorf("got %q, want %q", got, "ab")
	}
}

func TestLineWrap(t *testing.T) {
	s := feed(t, 4, 3, "abcdefg")
	lines := VisibleLines(s)
	if lines[0] != "abcd" || lines[1] != "efg" {
		t.Errorf("got %q / %q, want abcd / efg", lines[0], lines[1])
	}
}

func TestSGRBoldColor(t *testing.T) {
	// Bold green "X", reset, plain "Y".
	s := feed(t, 5, 1, "\x1b[1;32mX\x1b[0mY")
	row := s.Lines[0].Cells
	if !row[0].Bold || row[0].Fg.Kind != ColorPalette || row[0].Fg.Index != 2 {
		t.Errorf("cell 0 style wrong: %+v", row[0])
	}
	if row[1].Bold || row[1].Fg.Kind != ColorDefault {
		t.Errorf("cell 1 style wrong: %+v", row[1])
	}
}

func TestAltScreen(t *testing.T) {
	m := New(10, 2)
	if err := m.Feed([]byte("primary\r\n")); err != nil {
		t.Fatal(err)
	}
	// DECSET 1049 enters the alt screen but does not move the cursor —
	// position it explicitly with CUP so the assertion is unambiguous.
	if err := m.Feed([]byte("\x1b[?1049h\x1b[H")); err != nil {
		t.Fatal(err)
	}
	if err := m.Feed([]byte("ALT")); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if !s.AltScreen {
		t.Fatal("expected alt screen active")
	}
	if VisibleLines(s)[0] != "ALT" {
		t.Errorf("alt line 0 = %q", VisibleLines(s)[0])
	}
	if err := m.Feed([]byte("\x1b[?1049l")); err != nil {
		t.Fatal(err)
	}
	s = m.Snapshot()
	if s.AltScreen {
		t.Fatal("expected primary screen")
	}
	if VisibleLines(s)[0] != "primary" {
		t.Errorf("primary line 0 = %q after alt exit", VisibleLines(s)[0])
	}
}

func TestWideChar(t *testing.T) {
	// CJK: "あ" is width 2.
	s := feed(t, 6, 1, "aあb")
	row := s.Lines[0].Cells
	if row[0].Text != "a" || row[1].Width != 2 || row[1].Text != "あ" || row[2].Width != 0 || row[3].Text != "b" {
		t.Errorf("wide char layout wrong: %+v", row)
	}
}

func TestCombiningMark(t *testing.T) {
	// "e" + combining acute (U+0301).
	s := feed(t, 5, 1, "é")
	row := s.Lines[0].Cells
	if !strings.HasPrefix(row[0].Text, "e") || !strings.Contains(row[0].Text, "́") {
		t.Errorf("combining mark not attached: %+v", row[0])
	}
}

func TestResize(t *testing.T) {
	m := New(10, 3)
	if err := m.Feed([]byte("hello\r\nworld")); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(20, 5); err != nil {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.Size.Cols != 20 || s.Size.Rows != 5 {
		t.Errorf("size = %+v", s.Size)
	}
	if VisibleLines(s)[0] != "hello" || VisibleLines(s)[1] != "world" {
		t.Errorf("content lost on resize: %v", VisibleLines(s))
	}
}

func TestVisibleTextStripsTrailingSpaces(t *testing.T) {
	s := feed(t, 10, 1, "ab")
	if got := VisibleText(s); got != "ab" {
		t.Errorf("VisibleText = %q, want %q", got, "ab")
	}
}

func TestScrollOnOverflow(t *testing.T) {
	s := feed(t, 5, 2, "one\r\ntwo\r\nthree")
	lines := VisibleLines(s)
	if lines[0] != "two" || lines[1] != "three" {
		t.Errorf("scroll wrong: %v", lines)
	}
}

func TestCSIDeleteChars(t *testing.T) {
	s := feed(t, 10, 1, "abcdef\x1b[1;3H\x1b[2P")
	if got := VisibleLines(s)[0]; got != "abef" {
		t.Errorf("DCH gave %q, want %q", got, "abef")
	}
}

func TestSGRExplicitZeroResetsAfterBold(t *testing.T) {
	// "CSI 1 m X CSI 1;0 m Y" — Y should be plain (zero param after
	// semicolon used to be dropped by the parser).
	s := feed(t, 5, 1, "\x1b[1mX\x1b[1;0mY")
	row := s.Lines[0].Cells
	if !row[0].Bold {
		t.Errorf("cell 0 should be bold: %+v", row[0])
	}
	if row[1].Bold {
		t.Errorf("cell 1 should be plain (explicit reset lost): %+v", row[1])
	}
}

func TestSGR256ColorIndexZero(t *testing.T) {
	// CSI 38;5;0m sets fg to palette index 0 — the trailing zero is the
	// data, not "missing param."
	s := feed(t, 5, 1, "\x1b[38;5;0mZ")
	c := s.Lines[0].Cells[0]
	if c.Fg.Kind != ColorPalette || c.Fg.Index != 0 {
		t.Errorf("fg = %+v, want palette index 0", c.Fg)
	}
}

func TestCSIInsertChars(t *testing.T) {
	s := feed(t, 10, 1, "abcd\x1b[1;2H\x1b[2@")
	if got := VisibleLines(s)[0]; got != "a  bcd" {
		t.Errorf("ICH gave %q, want %q", got, "a  bcd")
	}
}
