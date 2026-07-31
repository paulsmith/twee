package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/input"
)

func TestMouseInputPTYEndToEnd(t *testing.T) {
	fixture := buildMouseFixture(t)

	t.Run("SGR gestures", func(t *testing.T) {
		term := startMouseFixture(t, fixture, "any", "sgr", 10, 100, 24)

		if err := term.Click(2, 1, input.ButtonLeft, nil); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=release button=none x=2 y=1 modifiers=")

		if err := term.Hover(3, 2, []input.MouseModifier{input.ModifierCtrl}); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=motion button=none x=3 y=2 modifiers=ctrl")

		if err := term.Scroll(4, 3, input.ScrollDown, 3, nil); err != nil {
			t.Fatal(err)
		}
		wheel := "EVENT action=press button=wheel_down x=4 y=3 modifiers="
		waitMouseEvent(t, term, wheel)

		if err := term.Drag(0, 0, 2, 0, input.ButtonRight, nil); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=release button=none x=2 y=0 modifiers=")

		if _, err := term.WaitForExit(); err != nil {
			t.Fatal(err)
		}
		if count := strings.Count(term.VisibleText(), wheel); count != 3 {
			t.Fatalf("wheel reports = %d, want 3\nscreen:\n%s", count, term.VisibleText())
		}
	})

	t.Run("X10 mode filters click release", func(t *testing.T) {
		term := startMouseFixture(t, fixture, "x10", "x10", 1, 80, 24)
		if err := term.Click(12, 4, input.ButtonMiddle, nil); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=press button=middle x=12 y=4 modifiers=")
		if _, err := term.WaitForExit(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("UTF8 large coordinate", func(t *testing.T) {
		term := startMouseFixture(t, fixture, "normal", "utf8", 2, 300, 24)
		if err := term.Click(223, 4, input.ButtonLeft, nil); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=release button=none x=223 y=4 modifiers=")
		if _, err := term.WaitForExit(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("URxvt release", func(t *testing.T) {
		term := startMouseFixture(t, fixture, "normal", "urxvt", 2, 80, 24)
		if err := term.Click(2, 3, input.ButtonRight, []input.MouseModifier{input.ModifierAlt}); err != nil {
			t.Fatal(err)
		}
		waitMouseEvent(t, term, "EVENT action=release button=none x=2 y=3 modifiers=alt")
		if _, err := term.WaitForExit(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRejectedMouseInputWritesZeroPTYBytes(t *testing.T) {
	const sentinel = "TWEE_MOUSE_SENTINEL"
	tests := []struct {
		name        string
		modes       string
		run         func(*Term) error
		wantErrKind RequestErrorKind
	}{
		{
			name:  "invalid coordinate",
			modes: "\x1b[?1003h\x1b[?1006h",
			run: func(term *Term) error {
				return term.Click(-1, 0, input.ButtonLeft, nil)
			},
			wantErrKind: RequestErrorInvalidArgument,
		},
		{
			name: "tracking disabled",
			run: func(term *Term) error {
				return term.Click(0, 0, input.ButtonLeft, nil)
			},
			wantErrKind: RequestErrorFailedPrecondition,
		},
		{
			name:  "SGR-Pixels",
			modes: "\x1b[?1003h\x1b[?1016h",
			run: func(term *Term) error {
				return term.Click(0, 0, input.ButtonLeft, nil)
			},
			wantErrKind: RequestErrorFailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The child reads exactly one canonical line after READY. Any
			// accidental mouse report written before the sentinel becomes
			// part of that line and changes CAPTURE_ZERO to CAPTURE_NONZERO.
			script := "stty -echo; printf '" + tt.modes + "READY\\r\\n'; " +
				"IFS= read -r captured; " +
				"if [ \"$captured\" = '" + sentinel + "' ]; then " +
				"printf 'CAPTURE_ZERO\\r\\n'; " +
				"else printf 'CAPTURE_NONZERO\\r\\n'; fi; " +
				"printf 'CAPTURE_DONE\\r\\n'"
			term := startEngineTerm(
				t,
				[]string{"/bin/sh", "-c", script},
				80,
				5,
			)
			if err := term.WaitForText("READY"); err != nil {
				t.Fatalf("WaitForText READY: %v\n%s", err, term.Diagnostic())
			}

			err := tt.run(term)
			var requestErr *RequestError
			if !errors.As(err, &requestErr) {
				t.Fatalf("mouse error = %v (%T), want *RequestError", err, err)
			}
			if requestErr.Kind != tt.wantErrKind {
				t.Fatalf("mouse error kind = %v, want %v", requestErr.Kind, tt.wantErrKind)
			}

			if err := term.Type(sentinel + "\n"); err != nil {
				t.Fatalf("write sentinel: %v", err)
			}
			if err := term.WaitForText("CAPTURE_DONE"); err != nil {
				t.Fatalf("wait for capture result: %v\n%s", err, term.Diagnostic())
			}
			screen := term.VisibleText()
			if !strings.Contains(screen, "CAPTURE_ZERO") {
				t.Fatalf("rejected gesture wrote child input before sentinel:\n%s", screen)
			}
			if _, err := term.WaitForExit(); err != nil {
				t.Fatalf("WaitForExit: %v", err)
			}
		})
	}
}

func buildMouseFixture(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	bin := filepath.Join(t.TempDir(), "mouse-fixture")
	cmd := exec.Command("go", "build", "-o", bin, "./fixtures/mouse")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mouse fixture: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root")
		}
		dir = parent
	}
}

func startMouseFixture(
	t *testing.T,
	fixture, tracking, format string,
	reports, cols, rows int,
) *Term {
	t.Helper()
	term, err := Start(context.Background(), Config{
		Cmd: []string{
			fixture,
			"--tracking", tracking,
			"--format", format,
			"--events", strconv.Itoa(reports),
		},
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.WaitForText("READY"); err != nil {
		t.Fatalf("wait for fixture READY: %v\n%s", err, term.Diagnostic())
	}
	return term
}

func waitMouseEvent(t *testing.T, term *Term, event string) {
	t.Helper()
	if err := term.WaitForText(event); err != nil {
		t.Fatalf("wait for %q: %v\n%s", event, err, term.Diagnostic())
	}
}
