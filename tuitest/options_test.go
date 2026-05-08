package tuitest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOptionsEnvDirDefaultTimeoutAndCursor(t *testing.T) {
	dir := t.TempDir()
	term := Run(t, "/bin/sh",
		Args("-c", "printf '%s\\r\\n%s\\r\\n' \"$(basename \"$PWD\")\" \"$TWEE_OPTION_TEST\"; sleep 30"),
		Dir(dir),
		Env("TWEE_OPTION_TEST", "present"),
		DefaultTimeout(2*time.Second),
		Size(50, 6))

	term.ExpectText(filepath.Base(dir))
	term.ExpectText("present")
	if got := term.Cursor(); !got.Visible {
		t.Fatalf("Cursor visible = false, want true")
	}
}

func TestRecordOptionWritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	term := Run(t, "/bin/sh",
		Args("-c", "printf 'ready\\r\\n'; sleep 30"),
		Record(path),
		Size(20, 4))

	term.ExpectText("ready")
	term.Type("abc")
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(b), `"type":"input"`) {
		t.Fatalf("recording missing input event:\n%s", b)
	}
}

func TestWithContextCancelsWait(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "sleep 30"), Size(20, 4))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := term.WaitForText("never", WithContext(ctx), WithTimeout(time.Second))
	if err == nil {
		t.Fatal("WaitForText unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestCtrlReExport(t *testing.T) {
	if Ctrl('c') != CtrlC {
		t.Fatalf("Ctrl('c') = %v, want CtrlC %v", Ctrl('c'), CtrlC)
	}
}
