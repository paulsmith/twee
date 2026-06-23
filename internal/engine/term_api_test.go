package engine

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/input"
)

func TestTermInputQueryAndWaitAPIs(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/cat"}, 20, 4)

	if got := te.Cmd(); strings.Join(got, " ") != "/bin/cat" {
		t.Fatalf("Cmd = %#v, want /bin/cat", got)
	}
	if te.DefaultTimeout() <= 0 {
		t.Fatalf("DefaultTimeout = %s, want positive", te.DefaultTimeout())
	}
	if te.StableQuietWindow() <= 0 {
		t.Fatalf("StableQuietWindow = %s, want positive", te.StableQuietWindow())
	}
	if te.StartedAt().IsZero() {
		t.Fatalf("StartedAt is zero")
	}

	if err := te.Type("hello"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := te.Key(input.KeyEnter); err != nil {
		t.Fatalf("Key: %v", err)
	}
	if err := te.WaitForText("hello", WithTimeout(2*time.Second)); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}
	if err := te.WaitForTextRegex(mustRegexp(t, `h.llo`), WithTimeout(2*time.Second)); err != nil {
		t.Fatalf("WaitForTextRegex: %v", err)
	}
	if err := te.WaitUntil(func(s Snapshot) bool {
		return s.Cols == 20 && s.Rows == 4 && strings.Contains(snapshotText(s), "hello")
	}, WithTimeout(2*time.Second)); err != nil {
		t.Fatalf("WaitUntil: %v", err)
	}
	if err := te.WaitForNoText("definitely-missing", WithTimeout(2*time.Second)); err != nil {
		t.Fatalf("WaitForNoText: %v", err)
	}
	c := te.CursorPos()
	if err := te.WaitForCursorAt(c.Col, c.Row, WithTimeout(2*time.Second)); err != nil {
		t.Fatalf("WaitForCursorAt: %v", err)
	}

	if err := te.Paste("paste-text"); err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if err := te.Resize(30, 6); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	snap := te.Snapshot()
	if snap.Cols != 30 || snap.Rows != 6 {
		t.Fatalf("Snapshot size = %dx%d, want 30x6", snap.Cols, snap.Rows)
	}
	if len(snap.Lines) != 6 {
		t.Fatalf("Snapshot lines = %d, want 6", len(snap.Lines))
	}
	if lines := te.Lines(); len(lines) != 6 {
		t.Fatalf("Lines = %d, want 6", len(lines))
	}
	if len(te.RecentBytes()) == 0 {
		t.Fatalf("RecentBytes is empty")
	}
	inputs := te.RecentInputs()
	if len(inputs) < 4 {
		t.Fatalf("RecentInputs = %#v, want type/key/paste/resize events", inputs)
	}
	if got := te.Diagnostic(); !strings.Contains(got, "recent input events") || !strings.Contains(got, "hello") {
		t.Fatalf("Diagnostic missing expected content:\n%s", got)
	}
	if err := te.Signal(os.Interrupt); err != nil {
		t.Fatalf("Signal: %v", err)
	}
}

func TestTermWaitForExit(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/sh", "-c", "exit 9"}, 10, 3)

	code, err := te.WaitForExit(WithTimeout(2 * time.Second))
	if err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if code != 9 {
		t.Fatalf("exit code = %d, want 9", code)
	}
	if got := te.ExitCode(); got != 9 {
		t.Fatalf("ExitCode = %d, want 9", got)
	}
	select {
	case <-te.ExitedCh():
	case <-time.After(2 * time.Second):
		t.Fatal("ExitedCh did not close")
	}
}

func TestTermWaitWithContextCancellation(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/sh", "-c", "sleep 30"}, 10, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := te.WaitForText("never", WithContext(ctx), WithTimeout(time.Second))
	if err == nil {
		t.Fatal("WaitForText unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestTermTraceLifecycle(t *testing.T) {
	te := startEngineTerm(t, []string{"/bin/cat"}, 20, 4)
	path := filepath.Join(t.TempDir(), "session.twee")

	if err := te.EnableTrace(path); err != nil {
		t.Fatalf("EnableTrace: %v", err)
	}
	if got := te.TracePath(); got != path {
		t.Fatalf("TracePath = %q, want %q", got, path)
	}
	if err := te.Type("traced"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := te.DisableTrace(); err != nil {
		t.Fatalf("DisableTrace: %v", err)
	}
	if got := te.TracePath(); got != "" {
		t.Fatalf("TracePath after disable = %q, want empty", got)
	}
	if err := te.DisableTrace(); err != nil {
		t.Fatalf("second DisableTrace: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("trace file: %v", err)
	}
}

func startEngineTerm(t *testing.T, cmd []string, cols, rows int) *Term {
	t.Helper()
	te, err := Start(context.Background(), Config{
		Cmd:  cmd,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	return te
}

func mustRegexp(t *testing.T, expr string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(expr)
	if err != nil {
		t.Fatal(err)
	}
	return re
}

func snapshotText(s Snapshot) string {
	lines := make([]string, len(s.Lines))
	for y, line := range s.Lines {
		var b strings.Builder
		for _, cell := range line.Cells {
			b.WriteString(cell.Text)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}
