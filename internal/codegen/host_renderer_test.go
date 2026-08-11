package codegen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/vt"
)

func renderOracle(t *testing.T, child vt.Model, rows int, status bool, line string) vt.Model {
	t.Helper()
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: rows, status: status}
	r.enter()
	r.render(child.Snapshot(), line, vt.Presentation{})
	host := vt.New(child.Snapshot().Size.Cols, rows)
	if err := host.Feed(b.Bytes()); err != nil {
		t.Fatal(err)
	}
	return host
}

func TestHostRendererKeepsStatusAfterScroll(t *testing.T) {
	child := vt.New(20, 3)
	_ = child.Feed([]byte("one\ntwo\nthree\nfour\n"))
	host := renderOracle(t, child, 4, true, "STATUS")
	lines := vt.VisibleLines(host.Snapshot())
	if lines[3] != "STATUS" {
		t.Fatalf("status=%q lines=%q", lines[3], lines)
	}
}

func TestHostRendererUsesCellDiffAfterInitialFrame(t *testing.T) {
	child := vt.New(20, 3)
	if err := child.Feed([]byte("alpha\r\nbeta")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := hostRenderer{w: &out, hostRows: 4, status: true}
	r.enter()
	r.render(child.Snapshot(), "STATUS", vt.Presentation{})

	host := vt.New(20, 4)
	if err := host.Feed(out.Bytes()); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := child.Feed([]byte("\x1b[2;1Hnext")); err != nil {
		t.Fatal(err)
	}
	r.render(child.Snapshot(), "STATUS", vt.Presentation{})
	update := append([]byte(nil), out.Bytes()...)
	if strings.Contains(string(update), "\x1b[2J") {
		t.Fatalf("incremental update cleared the screen: %q", update)
	}
	if len(update) >= 1000 {
		t.Fatalf("small update emitted %d bytes", len(update))
	}
	if err := host.Feed(update); err != nil {
		t.Fatal(err)
	}
	lines := vt.VisibleLines(host.Snapshot())
	if lines[0] != "alpha" || lines[1] != "next" || lines[3] != "STATUS" {
		t.Fatalf("lines after diff = %q", lines)
	}
}

func TestHostRendererStatusOnlyUpdateDoesNotRepaintChild(t *testing.T) {
	child := vt.New(20, 3)
	if err := child.Feed([]byte("body")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := hostRenderer{w: &out, hostRows: 4, status: true}
	r.enter()
	r.render(child.Snapshot(), "OLD", vt.Presentation{})
	host := vt.New(20, 4)
	if err := host.Feed(out.Bytes()); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	r.render(child.Snapshot(), "NEW", vt.Presentation{})
	update := append([]byte(nil), out.Bytes()...)
	if strings.Contains(string(update), "\x1b[2J") || len(update) >= 300 {
		t.Fatalf("status-only update repainted child: len=%d bytes=%q", len(update), update)
	}
	if err := host.Feed(update); err != nil {
		t.Fatal(err)
	}
	snapshot := host.Snapshot()
	lines := vt.VisibleLines(snapshot)
	if lines[0] != "body" || lines[3] != "NEW" {
		t.Fatalf("status-only lines = %q", lines)
	}
	if cells := snapshot.Lines[3].Cells; len(cells) != 20 || !cells[0].Inverse || !cells[19].Inverse {
		t.Fatalf("status style does not span footer: %#v", cells)
	}
}

func TestHostRendererContainsChildModes(t *testing.T) {
	child := vt.New(20, 3)
	_ = child.Feed([]byte("\x1b[2;3r\x1b[?6h\x1b[?1049hALT\x1b[?1049l\x1b[r\x1b[?6lbody"))
	host := renderOracle(t, child, 4, true, "STATUS")
	s := host.Snapshot()
	if !s.AltScreen {
		t.Fatal("wrapper alt screen missing")
	}
	lines := vt.VisibleLines(s)
	if lines[3] != "STATUS" {
		t.Fatalf("status=%q", lines[3])
	}
}

func TestHostRendererCursorAndClose(t *testing.T) {
	child := vt.New(20, 3)
	_ = child.Feed([]byte("\x1b[?25l"))
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: 4, status: true, preserve: true}
	r.enter()
	r.render(child.Snapshot(), "STATUS", vt.Presentation{})
	host := vt.New(20, 4)
	_ = host.Feed(b.Bytes())
	if host.Snapshot().Cursor.Visible {
		t.Fatal("hidden cursor lost")
	}
	b.Reset()
	r.close()
	_ = host.Feed(b.Bytes())
	s := host.Snapshot()
	if !s.Cursor.Visible || s.AltScreen {
		t.Fatalf("close=%+v", s.Cursor)
	}
}

func TestHostRendererOneRowThenStatus(t *testing.T) {
	child := vt.New(12, 1)
	_ = child.Feed([]byte("x"))
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: 1, status: false}
	r.enter()
	r.render(child.Snapshot(), "ignored", vt.Presentation{})
	host := vt.New(12, 1)
	_ = host.Feed(b.Bytes())
	if len(vt.VisibleLines(host.Snapshot())) != 1 {
		t.Fatal("one row")
	}
	if !r.active {
		t.Fatal("renderer exited")
	}
	b.Reset()
	r.hostRows = 2
	r.status = true
	_ = child.Resize(12, 1)
	r.render(child.Snapshot(), "STATUS", vt.Presentation{})
	_ = host.Resize(12, 2)
	_ = host.Feed(b.Bytes())
	if got := vt.VisibleLines(host.Snapshot())[1]; got != "STATUS" {
		t.Fatalf("status=%q", got)
	}
}

func TestHostRendererMirrorsOnlyInputModeDeltasAndCleansUp(t *testing.T) {
	child := vt.New(20, 3)
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: 4, status: true, preserve: true}
	r.enter()
	b.Reset()
	p := vt.Presentation{Input: vt.InputModes{
		ApplicationCursor: true, ApplicationKeypad: true, BracketedPaste: true, FocusEvents: true,
		MouseNormal: true, MouseSGR: true, MouseSGRPixels: true,
	}}
	r.render(child.Snapshot(), "STATUS", p)
	first := b.String()
	for _, seq := range []string{"\x1b[?1h", "\x1b[?66h", "\x1b[?2004h", "\x1b[?1004h", "\x1b[?1000h", "\x1b[?1006h"} {
		if !strings.Contains(first, seq) {
			t.Fatalf("missing enabled mode %q in %q", seq, first)
		}
	}
	if strings.Contains(first, "\x1b[?1016h") {
		t.Fatalf("pixel mouse must not be mirrored: %q", first)
	}
	b.Reset()
	r.render(child.Snapshot(), "STATUS", p)
	if got := b.String(); strings.Contains(got, "\x1b[?1h") || strings.Contains(got, "\x1b[?2004h") || strings.Contains(got, "\x1b[?66h") {
		t.Fatalf("unchanged modes were mirrored again: %q", got)
	}
	b.Reset()
	r.close()
	cleanup := b.String()
	for _, seq := range []string{"\x1b[?1l", "\x1b[?66l", "\x1b[?2004l", "\x1b[?1004l", "\x1b[?1000l", "\x1b[?1006l", "\x1b[?1049l", "\x1b[?" + hostPrivateInputModes + "r"} {
		if !strings.Contains(cleanup, seq) {
			t.Fatalf("missing cleanup %q in %q", seq, cleanup)
		}
	}
}

func TestHostRendererMirrorsCursorStyleInsideAlternateScreen(t *testing.T) {
	child := vt.New(20, 3)
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: 4, status: true}
	r.enter()
	b.Reset()
	_ = child.Feed([]byte("\x1b[6 q"))
	r.render(child.Snapshot(), "STATUS", vt.Presentation{Cursor: child.Snapshot().Cursor.Style})
	_ = child.Feed([]byte("\x1b[4 q"))
	r.render(child.Snapshot(), "STATUS", vt.Presentation{Cursor: child.Snapshot().Cursor.Style})
	if !strings.Contains(b.String(), "\x1b[6 q") || !strings.Contains(b.String(), "\x1b[4 q") {
		t.Fatalf("child cursor style not represented in alternate screen: %q", b.String())
	}
}

func TestHostRendererRestoresSavedHostState(t *testing.T) {
	host := vt.New(20, 4)
	if err := host.Feed([]byte("\x1b[?1h\x1b=\x1b[?2004h\x1b[?1004h\x1b[?1000h\x1b[?1006h\x1b[?25l\x1b[6 q")); err != nil {
		t.Fatal(err)
	}
	want, err := host.(vt.PresentationSource).Presentation()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	r := hostRenderer{w: &b, hostRows: 4, status: true, preserve: true}
	r.enter()
	child := vt.New(20, 3)
	r.render(child.Snapshot(), "STATUS", vt.Presentation{})
	r.close()
	if err := host.Feed(b.Bytes()); err != nil {
		t.Fatal(err)
	}
	got, err := host.(vt.PresentationSource).Presentation()
	if err != nil {
		t.Fatal(err)
	}
	if got.Input != want.Input || host.Snapshot().Cursor.Style != vt.CursorStyleBar || host.Snapshot().Cursor.Visible {
		t.Fatalf("restored presentation=%+v cursor=%+v", got, host.Snapshot().Cursor)
	}
	if !strings.Contains(b.String(), "\x1b[?"+hostPrivateInputModes+"s") || !strings.Contains(b.String(), "\x1b[?"+hostPrivateInputModes+"r") {
		t.Fatalf("native save/restore missing: %q", b.String())
	}
}
