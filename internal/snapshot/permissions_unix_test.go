//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package snapshot

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCompareTextCreatesPrivateSnapshot(t *testing.T) {
	oldUmask := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	path := filepath.Join(t.TempDir(), "snapshots", "screen.txt")
	if _, err := CompareText(path, "secret terminal output\n", false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %04o, want 0600", path, got)
	}
}
