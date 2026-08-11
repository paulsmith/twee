package inspect

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func TestReplayFinalSemanticState(t *testing.T) {
	bundle := tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 8, Rows: 3},
		Events: []tracebundle.Event{
			{TMS: 10, Type: trace.EventTypeResize, Cols: 10, Rows: 4},
			{TMS: 20, Type: trace.EventTypeOutput, Bytes: []byte(strings.Join([]string{
				"\x1b[?1049h",
				"\x1b[?1h",
				"\x1b=",
				"\x1b[?2004h",
				"\x1b[?1004h",
				"\x1b[?1000;1006h",
				"\x1b[?25l",
				"\x1b[6 q",
				"\x1b[2;3H",
				"\x1b[38;5;99;48;2;1;2;3;1;2;3;4;7;9mX",
			}, ""))},
		},
	}

	got, err := Replay(bundle)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if got.Final.EventIndex == nil || *got.Final.EventIndex != 1 || got.Final.TMS == nil || *got.Final.TMS != 20 {
		t.Fatalf("final event = %v at %v", got.Final.EventIndex, got.Final.TMS)
	}
	if got.Final.Size.Cols != 10 || got.Final.Size.Rows != 4 {
		t.Fatalf("final size = %+v", got.Final.Size)
	}
	if !got.Final.AltScreen || !got.Final.Modes.AltScreen || !got.Final.Modes.DECCKM || !got.Final.Modes.ApplicationKeypad || !got.Final.Modes.BracketedPaste || !got.Final.Modes.FocusEvents {
		t.Fatalf("final modes = %+v", got.Final.Modes)
	}
	if got.Final.Modes.Mouse || got.Final.Modes.MouseKnown || !got.Final.Modes.MouseRaw || !got.Final.Modes.MouseTrackingNormal || !got.Final.Modes.MouseFormatSGR {
		t.Fatalf("final mouse modes = %+v", got.Final.Modes)
	}
	if got.Final.Cursor.X != 3 || got.Final.Cursor.Y != 1 || got.Final.Cursor.Visible || got.Final.Cursor.Shape != "bar" {
		t.Fatalf("final cursor = %+v", got.Final.Cursor)
	}
	if !strings.Contains(got.Final.VisibleText, "  X") {
		t.Fatalf("visible text = %q", got.Final.VisibleText)
	}
	cell := replayTestCellAt(t, got.Final.Lines[1], 2)
	if cell.Text != "X" || cell.Fg.Kind != "palette" || cell.Fg.Index == nil || *cell.Fg.Index != 99 ||
		cell.Bg.Kind != "rgb" || cell.Bg.R != 1 || cell.Bg.G != 2 || cell.Bg.B != 3 ||
		!cell.Bold || !cell.Dim || !cell.Italic || !cell.Underline || !cell.Inverse || !cell.Strikethrough {
		t.Fatalf("styled cell = %+v", cell)
	}
	if got.Final.StyledCells < 1 {
		t.Fatalf("styled cells = %d", got.Final.StyledCells)
	}
	if len(got.ModeTransitions) == 0 {
		t.Fatal("mode transitions are empty")
	}
}

func replayTestCellAt(t *testing.T, line ReplayLine, x int) rpc.CellData {
	t.Helper()
	for _, run := range line.Runs {
		if x < run.Count {
			return run.Cell
		}
		x -= run.Count
	}
	t.Fatalf("cell x=%d outside replay line", x)
	return rpc.CellData{}
}

func TestReplayCapturesTransitionsWithinOneOutputEvent(t *testing.T) {
	const sequence = "\x1b[?2004h\x1b[?2004l\x1b[?2004h"
	got, err := Replay(tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 10, Rows: 3},
		Events:   []tracebundle.Event{{TMS: 25, Type: trace.EventTypeOutput, Bytes: []byte(sequence)}},
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got.ModeTransitions) != 3 {
		t.Fatalf("transitions = %+v, want 3", got.ModeTransitions)
	}
	for i, wantOffset := range []int{7, 15, 23} {
		transition := got.ModeTransitions[i]
		if transition.TMS != 25 || transition.EventIndex != 0 || transition.ByteOffset != wantOffset || !reflect.DeepEqual(transition.Changed, []string{"bracketed_paste"}) {
			t.Fatalf("transition %d = %+v", i, transition)
		}
	}
	if !got.ModeTransitions[0].Modes.BracketedPaste || got.ModeTransitions[1].Modes.BracketedPaste || !got.ModeTransitions[2].Modes.BracketedPaste {
		t.Fatalf("transition states = %+v", got.ModeTransitions)
	}
}

func TestReplayCapturesSplitControlSequenceAtCompletingEvent(t *testing.T) {
	got, err := Replay(tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 10, Rows: 3},
		Events: []tracebundle.Event{
			{TMS: 10, Type: trace.EventTypeOutput, Bytes: []byte("\x1b[?200")},
			{TMS: 15, Type: trace.EventTypeInput, Bytes: []byte("ignored")},
			{TMS: 20, Type: trace.EventTypeOutput, Bytes: []byte("4h")},
		},
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got.ModeTransitions) != 1 {
		t.Fatalf("transitions = %+v, want 1", got.ModeTransitions)
	}
	transition := got.ModeTransitions[0]
	if transition.TMS != 20 || transition.EventIndex != 2 || transition.ByteOffset != 1 || !reflect.DeepEqual(transition.Changed, []string{"bracketed_paste"}) {
		t.Fatalf("transition = %+v", transition)
	}
}

func TestReplayGroupsModesChangedByOneSequence(t *testing.T) {
	got, err := Replay(tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 10, Rows: 3},
		Events: []tracebundle.Event{{
			TMS: 30, Type: trace.EventTypeOutput, Bytes: []byte("\x1b[?1;2004h"),
		}},
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got.ModeTransitions) != 1 {
		t.Fatalf("transitions = %+v, want 1", got.ModeTransitions)
	}
	if want := []string{"bracketed_paste", "decckm"}; !reflect.DeepEqual(got.ModeTransitions[0].Changed, want) {
		t.Fatalf("changed = %v, want %v", got.ModeTransitions[0].Changed, want)
	}
}

func TestReplayEmptyTraceReturnsInitialState(t *testing.T) {
	got, err := Replay(tracebundle.Bundle{Manifest: trace.Manifest{Version: 1, Cols: 4, Rows: 2}})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if got.Final.EventIndex != nil || got.Final.TMS != nil {
		t.Fatalf("final provenance = %v at %v", got.Final.EventIndex, got.Final.TMS)
	}
	if got.Final.Size.Cols != 4 || got.Final.Size.Rows != 2 || got.Final.AltScreen {
		t.Fatalf("final = %+v", got.Final)
	}
	if got.ModeTransitions == nil || len(got.ModeTransitions) != 0 {
		t.Fatalf("transitions = %#v, want non-nil empty", got.ModeTransitions)
	}
	if got.Final.VisibleText != "\n" {
		t.Fatalf("visible text = %q, want one row separator", got.Final.VisibleText)
	}
}

func TestReplayRunLengthEncodesLargeBlankViewport(t *testing.T) {
	got, err := Replay(tracebundle.Bundle{Manifest: trace.Manifest{Version: 1, Cols: 1000, Rows: 100}})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(got.Final.Lines) != 100 {
		t.Fatalf("lines = %d, want 100", len(got.Final.Lines))
	}
	for row, line := range got.Final.Lines {
		if len(line.Runs) != 1 || line.Runs[0].Count != 1000 {
			t.Fatalf("row %d runs = %+v", row, line.Runs)
		}
	}
}

func TestReplayRejectsOversizedEncodedState(t *testing.T) {
	_, err := Replay(tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 1000, Rows: 100},
		Events: []tracebundle.Event{{
			TMS: 1, Type: trace.EventTypeOutput, Bytes: bytes.Repeat([]byte("ab"), 50_000),
		}},
	})
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || !strings.Contains(limitErr.Error(), "encoded replay exceeds") {
		t.Fatalf("Replay error = %v, want encoded-size LimitError", err)
	}
}

func TestReplayScannerMatchesUTF8AndCancellationRecovery(t *testing.T) {
	for _, output := range [][]byte{
		[]byte("Ý\x1b[?2004h"),
		append([]byte{0x9d}, []byte("\x1b[?2004h")...),
		[]byte("\x1b]title\x18\x1b[?2004h"),
		[]byte("\x1bPpayload\x1a\x1b[?2004h"),
		[]byte("\x1b]unterminated\x1b[?2004h"),
		[]byte("\x1bPunterminated\x1b[?2004h"),
	} {
		got, err := Replay(tracebundle.Bundle{
			Manifest: trace.Manifest{Version: 1, Cols: 20, Rows: 2},
			Events:   []tracebundle.Event{{TMS: 10, Type: trace.EventTypeOutput, Bytes: output}},
		})
		if err != nil {
			t.Fatalf("Replay(%q): %v", output, err)
		}
		if !got.Final.Modes.BracketedPaste || len(got.ModeTransitions) != 1 || !reflect.DeepEqual(got.ModeTransitions[0].Changed, []string{"bracketed_paste"}) {
			t.Fatalf("Replay(%q) transitions = %+v, final modes = %+v", output, got.ModeTransitions, got.Final.Modes)
		}
	}
}

func TestReplayScannerRecoversFromFragmentedStringEscape(t *testing.T) {
	for _, prefix := range [][]byte{
		[]byte("\x1b]unterminated\x1b"),
		[]byte("\x1bPunterminated\x1b"),
	} {
		got, err := Replay(tracebundle.Bundle{
			Manifest: trace.Manifest{Version: 1, Cols: 20, Rows: 2},
			Events: []tracebundle.Event{
				{TMS: 10, Type: trace.EventTypeOutput, Bytes: prefix},
				{TMS: 20, Type: trace.EventTypeOutput, Bytes: []byte("[?2004h")},
			},
		})
		if err != nil {
			t.Fatalf("Replay(%q): %v", prefix, err)
		}
		if len(got.ModeTransitions) != 1 {
			t.Fatalf("Replay(%q) transitions = %+v", prefix, got.ModeTransitions)
		}
		transition := got.ModeTransitions[0]
		if transition.TMS != 20 || transition.EventIndex != 1 || transition.ByteOffset != 6 || !reflect.DeepEqual(transition.Changed, []string{"bracketed_paste"}) {
			t.Fatalf("Replay(%q) transition = %+v", prefix, transition)
		}
	}
}

func TestControlSequenceScannerRecognizesStringTerminators(t *testing.T) {
	for _, sequence := range [][]byte{
		[]byte("\x1b]title\a"),
		[]byte("\x1b]title\x1b\\"),
		[]byte("\x1bPpayload\x1b\\"),
		[]byte("\x1b]title\x18\x1b[?2004h"),
	} {
		scanner := controlSequenceScanner{}
		completed := 0
		for _, b := range sequence {
			if scanner.step(b) {
				completed++
			}
		}
		if completed != 1 {
			t.Fatalf("sequence %q completed %d controls, want 1", sequence, completed)
		}
	}
}
