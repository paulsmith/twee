package vt

import (
	"strings"
	"testing"
)

func TestPTYRepliesDrain(t *testing.T) {
	m := New(80, 24)
	if err := m.Feed([]byte("\x1b[6n")); err != nil {
		t.Fatal(err)
	}
	r, ok := m.(PTYReplySource)
	if !ok {
		t.Fatal("no reply source")
	}
	got := r.DrainPTYReplies()
	if len(got) == 0 || len(got[0]) == 0 {
		t.Fatalf("replies=%q", got)
	}
	if again := r.DrainPTYReplies(); len(again) != 0 {
		t.Fatalf("not drained: %q", again)
	}
}

func TestPTYReplyDoesNotConsumeFollowingOutput(t *testing.T) {
	m := New(80, 24)
	if err := m.Feed([]byte("\x1b[6n")); err != nil {
		t.Fatal(err)
	}
	if err := m.Feed([]byte("GOT-CPR:$'\\E[1;1R'\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := VisibleText(m.Snapshot()); !strings.Contains(got, "GOT-CPR:") {
		t.Fatalf("visible=%q", got)
	}
}

func TestPresentationInputModesAndCursorStyle(t *testing.T) {
	m := New(80, 24)
	if err := m.Feed([]byte("\x1b[?1h\x1b=\x1b[?2004h\x1b[?1004h\x1b[?9h\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1005h\x1b[?1006h\x1b[?1015h\x1b[?1016h\x1b[6 q")); err != nil {
		t.Fatal(err)
	}
	p, ok := m.(PresentationSource)
	if !ok {
		t.Fatal("no presentation source")
	}
	got := p.Presentation()
	if !got.Input.ApplicationCursor || !got.Input.ApplicationKeypad || !got.Input.BracketedPaste || !got.Input.FocusEvents ||
		!got.Input.MouseX10 || !got.Input.MouseNormal || !got.Input.MouseButton || !got.Input.MouseAny ||
		!got.Input.MouseUTF8 || !got.Input.MouseSGR || !got.Input.MouseURxvt || !got.Input.MouseSGRPixels {
		t.Fatalf("modes=%+v", got.Input)
	}
	if got.Cursor != CursorStyleBar || m.Snapshot().Cursor.Style != CursorStyleBar {
		t.Fatalf("bar presentation=%v snapshot=%v", got.Cursor, m.Snapshot().Cursor.Style)
	}
	if err := m.Feed([]byte("\x1b[?1l\x1b>\x1b[?2004l\x1b[?1004l\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1005l\x1b[?1006l\x1b[?1015l\x1b[?1016l\x1b[4 q")); err != nil {
		t.Fatal(err)
	}
	got = p.Presentation()
	if got.Input != (InputModes{}) || got.Cursor != CursorStyleUnderline || m.Snapshot().Cursor.Style != CursorStyleUnderline {
		t.Fatalf("presentation=%+v snapshot=%v", got, m.Snapshot().Cursor.Style)
	}
}

func TestPrivateModeSaveRestoreAndAltCursorStyle(t *testing.T) {
	m := New(80, 24)
	if err := m.Feed([]byte("\x1b[?1h\x1b=\x1b[?9h\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1004h\x1b[?1005h\x1b[?1006h\x1b[?1015h\x1b[?1016h\x1b[?2004h\x1b[?25l\x1b[6 q")); err != nil {
		t.Fatal(err)
	}
	p := m.(PresentationSource).Presentation()
	if err := m.Feed([]byte("\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004s\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004l\x1b[?25h\x1b[?1049h\x1b[4 q\x1b[?1049l\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004r")); err != nil {
		t.Fatal(err)
	}
	got := m.(PresentationSource).Presentation()
	if got.Input != p.Input {
		t.Fatalf("modes=%+v want=%+v", got.Input, p.Input)
	}
	if m.Snapshot().Cursor.Visible {
		t.Fatal("private-mode restore did not restore hidden cursor")
	}
	if got.Cursor != p.Cursor {
		t.Fatalf("alternate screen did not restore cursor style: %v want %v", got.Cursor, p.Cursor)
	}
}
