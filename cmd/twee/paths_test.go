package main

import (
	"os"
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

func TestAbsOutPath(t *testing.T) {
	if got, err := absOutPath(""); err != nil || got != "" {
		t.Fatalf("absOutPath(\"\") = %q, %v; want empty, nil", got, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	got, err := absOutPath("shot.png")
	if err != nil {
		t.Fatalf("absOutPath: %v", err)
	}
	want := filepath.Join(wd, "shot.png")
	if got != want {
		t.Errorf("absOutPath(\"shot.png\") = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("absOutPath result %q is not absolute", got)
	}
	// Already-absolute paths pass through unchanged.
	if got, err := absOutPath("/tmp/x.png"); err != nil || got != "/tmp/x.png" {
		t.Errorf("absOutPath(\"/tmp/x.png\") = %q, %v; want \"/tmp/x.png\", nil", got, err)
	}
}

func TestNameValidation(t *testing.T) {
	for _, bad := range []string{"", "../etc", "foo/bar", "a\x00b"} {
		if _, err := socketPath(bad); err == nil {
			t.Errorf("expected error for name %q", bad)
		}
	}
}
