package codegen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/vt"
)

func TestNextHotkeyTracePath(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ops.json")
	now := time.Date(2026, 5, 5, 14, 3, 9, 0, time.UTC)

	got, err := nextHotkeyTracePath(out, now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "ops-trace-20260505-140309.twee")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestNextHotkeyTracePathCollisionSuffix(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "ops.json")
	now := time.Date(2026, 5, 5, 14, 3, 9, 0, time.UTC)
	for _, name := range []string{
		"ops-trace-20260505-140309.twee",
		"ops-trace-20260505-140309-02.twee",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := nextHotkeyTracePath(out, now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "ops-trace-20260505-140309-03.twee")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestTraceControllerToggleDoesNotStopFullSessionTrace(t *testing.T) {
	var stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "full.twee")
	c := newTraceController(Options{
		Command: []string{"/bin/cat"},
		OutPath: "ops.json",
		Stderr:  &stderr,
	}, 80, 24, 123)
	if err := c.startFullSession(path, vt.New(80, 24).Snapshot()); err != nil {
		t.Fatal(err)
	}

	if err := c.toggleHotkey(vt.New(80, 24).Snapshot()); err != nil {
		t.Fatal(err)
	}
	if c.mode != traceModeFullSession || c.path != path || c.tr == nil {
		t.Fatalf("trace state = mode %v path %q tr nil %v", c.mode, c.path, c.tr == nil)
	}
	if !strings.Contains(stderr.String(), "already tracing full session: "+path) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
}
