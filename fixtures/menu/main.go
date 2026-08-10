// Fixture: a tiny menu TUI driven entirely by ANSI escapes (no Bubble Tea).
//
// On startup it prints "Choose an option" and three menu items. Up/Down
// move the highlight; Enter prints "selected: <item>" and exits.
//
// Stdin is set to raw mode so individual keypresses arrive without
// requiring Enter to flush a line.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

var items = []string{"first", "second", "third"}

func main() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "menu: not a tty")
		os.Exit(2)
	}
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer term.Restore(int(os.Stdin.Fd()), old)

	sel := 0
	render(sel)

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		// Recognize: \r (Enter), ESC[A (Up), ESC[B (Down), Ctrl+C (0x03).
		s := string(buf[:n])
		switch {
		case s == "\x03":
			return
		case s == "\r" || s == "\n":
			term.Restore(int(os.Stdin.Fd()), old)
			fmt.Printf("\r\nselected: %s\r\n", items[sel])
			return
		case s == "\x1b[A":
			if sel > 0 {
				sel--
			}
		case s == "\x1b[B":
			if sel < len(items)-1 {
				sel++
			}
		}
		render(sel)
	}
}

func render(sel int) {
	// Clear screen, home cursor.
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Print("Choose an option\r\n")
	for i, it := range items {
		if i == sel {
			fmt.Printf("> %s\r\n", it)
		} else {
			fmt.Printf("  %s\r\n", it)
		}
	}
}
