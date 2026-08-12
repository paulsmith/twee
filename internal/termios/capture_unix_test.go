//go:build linux || darwin

package termios

import (
	"os"
	"testing"
)

func TestCaptureUnavailableForNonTerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	snapshot := Capture(file.Fd())
	if snapshot.Status != StatusUnavailable || snapshot.State != nil || snapshot.Error == "" {
		t.Fatalf("snapshot = %+v, want explicit unavailable status", snapshot)
	}
}
