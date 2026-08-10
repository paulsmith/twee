//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package codegen

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWrapScriptsAreCreatedPrivate(t *testing.T) {
	oldUmask := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.json")
	if err := writeScript(explicit, nil); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, explicit)

	generated, err := nextRecorderPathInDir(dir, "twee-script", ".json", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeScript(generated, nil); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, generated)
}

func assertPrivateMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %04o, want 0600", path, got)
	}
}
