package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSocketPathDefault(t *testing.T) {
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)
	t.Setenv("HOME", home)
	got, err := socketPath("default")
	if err != nil {
		t.Fatalf("socketPath: %v", err)
	}
	want := filepath.Join(xdg, "twee", "default.sock")
	if runtime.GOOS == "darwin" {
		want = filepath.Join(home, "Library", "Application Support", "twee", "default.sock")
	}
	if got != want {
		t.Errorf("socketPath = %q, want %q", got, want)
	}
}

func TestSocketPathFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("TMPDIR", tmp)
	t.Setenv("USER", "alice")
	got, err := socketPath("foo")
	if err != nil {
		t.Fatalf("socketPath: %v", err)
	}
	if !strings.HasPrefix(got, tmp) {
		t.Errorf("expected fallback under %q, got %q", tmp, got)
	}
	if !strings.HasSuffix(got, "foo.sock") {
		t.Errorf("expected .sock for name foo, got %q", got)
	}
}

func TestNameValidation(t *testing.T) {
	for _, bad := range []string{"", "../etc", "foo/bar", "a\x00b"} {
		if _, err := socketPath(bad); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}
