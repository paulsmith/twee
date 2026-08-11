package vt

import (
	"strings"
	"testing"
)

func mustPresentation(t *testing.T, source PresentationSource) Presentation {
	t.Helper()
	presentation, err := source.Presentation()
	if err != nil {
		t.Fatal(err)
	}
	return presentation
}

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
	got := mustPresentation(t, p)
	if !got.Input.KittyKeyboardKnown || got.Input.KittyKeyboardFlags != 0 {
		t.Fatalf("default Kitty keyboard state=%+v, want known disabled", got.Input)
	}
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
	got = mustPresentation(t, p)
	if got.Input != (InputModes{KittyKeyboardKnown: true}) || got.Cursor != CursorStyleUnderline || m.Snapshot().Cursor.Style != CursorStyleUnderline {
		t.Fatalf("presentation=%+v snapshot=%v", got, m.Snapshot().Cursor.Style)
	}
}

func TestPresentationTracksFragmentedBracketedPasteTransitions(t *testing.T) {
	m := New(80, 24)
	p := m.(PresentationSource)
	for _, fragment := range []string{"\x1b", "[?20"} {
		if err := m.Feed([]byte(fragment)); err != nil {
			t.Fatal(err)
		}
		if got := mustPresentation(t, p).Input.BracketedPaste; got {
			t.Fatalf("bracketed paste enabled before sequence completed after %q", fragment)
		}
	}
	if err := m.Feed([]byte("04h")); err != nil {
		t.Fatal(err)
	}
	if got := mustPresentation(t, p).Input.BracketedPaste; !got {
		t.Fatal("bracketed paste disabled after fragmented DECSET 2004")
	}
	for _, fragment := range []string{"\x1b[?2", "004", "l"} {
		if err := m.Feed([]byte(fragment)); err != nil {
			t.Fatal(err)
		}
	}
	if got := mustPresentation(t, p).Input.BracketedPaste; got {
		t.Fatal("bracketed paste enabled after fragmented DECRST 2004")
	}
}

func TestPresentationReportsKittyKeyboardFlags(t *testing.T) {
	m := New(80, 24)
	p := m.(PresentationSource)
	if err := m.Feed([]byte("\x1b[>1u")); err != nil {
		t.Fatal(err)
	}
	if got := mustPresentation(t, p).Input; !got.KittyKeyboardKnown || got.KittyKeyboardFlags != 1 {
		t.Fatalf("active Kitty keyboard state=%+v, want known flags=1", got)
	}
	if err := m.Feed([]byte("\x1b[<u")); err != nil {
		t.Fatal(err)
	}
	if got := mustPresentation(t, p).Input; !got.KittyKeyboardKnown || got.KittyKeyboardFlags != 0 {
		t.Fatalf("reset Kitty keyboard state=%+v, want known disabled", got)
	}
}

func TestPrivateModeSaveRestoreAndAltCursorStyle(t *testing.T) {
	m := New(80, 24)
	if err := m.Feed([]byte("\x1b[?1h\x1b=\x1b[?9h\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1004h\x1b[?1005h\x1b[?1006h\x1b[?1015h\x1b[?1016h\x1b[?2004h\x1b[?25l\x1b[6 q")); err != nil {
		t.Fatal(err)
	}
	p := mustPresentation(t, m.(PresentationSource))
	if err := m.Feed([]byte("\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004s\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004l\x1b[?25h\x1b[?1049h\x1b[4 q\x1b[?1049l\x1b[?1;9;25;66;1000;1002;1003;1004;1005;1006;1015;1016;2004r")); err != nil {
		t.Fatal(err)
	}
	got := mustPresentation(t, m.(PresentationSource))
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
