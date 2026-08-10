//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package render

import (
	"image"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEncodePNGFileCreatesPrivateFile(t *testing.T) {
	oldUmask := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	path := filepath.Join(t.TempDir(), "screen.png")
	if err := EncodePNGFile(path, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, path)
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
