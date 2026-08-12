package main

import (
	"os"
	"testing"

	"github.com/creack/pty"
)

func TestDisplayPixelsFromPTY(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer func() { _ = master.Close() }()
	defer func() { _ = slave.Close() }()

	if err := pty.Setsize(slave, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    1234,
		Y:    567,
	}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}

	w, h, ok := displayPixelsFromFile(slave)
	if !ok {
		t.Fatal("displayPixelsFromFile returned ok=false")
	}
	if w != 1234 || h != 567 {
		t.Fatalf("display pixels = %dx%d, want 1234x567", w, h)
	}
}

func TestDisplayPixelsFromFileRejectsZeroPixels(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-tty")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, _, ok := displayPixelsFromFile(f); ok {
		t.Fatal("displayPixelsFromFile returned ok=true for a regular file")
	}
}
