package main

import (
	"os"

	"github.com/creack/pty"
)

func nativeDisplayPixels() (int, int) {
	for _, f := range []*os.File{os.Stdout, os.Stdin, os.Stderr} {
		if w, h, ok := displayPixelsFromFile(f); ok {
			return w, h
		}
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = tty.Close() }()
	if w, h, ok := displayPixelsFromFile(tty); ok {
		return w, h
	}
	return 0, 0
}

func displayPixelsFromFile(f *os.File) (int, int, bool) {
	if f == nil {
		return 0, 0, false
	}
	ws, err := pty.GetsizeFull(f)
	if err != nil || ws == nil || ws.X == 0 || ws.Y == 0 {
		return 0, 0, false
	}
	return int(ws.X), int(ws.Y), true
}
