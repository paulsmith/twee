package tuitest

import (
	"testing"
	"time"
)

func TestWaitForText(t *testing.T) {
	// sh that prints "ready" after 50ms, then "done" after 100ms.
	term := Run(t, "/bin/sh",
		Args("-c", "sleep 0.05; printf 'ready\\r\\n'; sleep 0.05; printf 'done\\r\\n'"),
		Size(40, 5))
	if err := term.WaitForText("ready", WithTimeout(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitForText("done", WithTimeout(2*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForTextTimeout(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "sleep 30"), Size(40, 5))
	err := term.WaitForText("never", WithTimeout(100*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestExpectText(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'pushup\\r\\nSaved\\r\\n'"), Size(40, 5))
	term.ExpectText("pushup")
	term.ExpectText("Saved")
	term.ExpectNoText("panic")
}

func TestWaitForStableScreen(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'hi\\r\\n'"), Size(40, 5))
	if err := term.WaitForStableScreen(50*time.Millisecond, WithTimeout(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := term.VisibleText(); !contains(got, "hi") {
		t.Errorf("got %q", got)
	}
}

// Regression for the first-paint race: WaitForStableScreen must not
// return success before the child has produced any output, even if
// quietFor elapses. Previously lastFeed was initialized to time.Now()
// so a 50ms wait against an app that took 200ms to first-paint would
// declare stability on a blank screen.
func TestWaitForStableScreenWaitsForFirstPaint(t *testing.T) {
	term := Run(t, "/bin/sh",
		Args("-c", "sleep 0.2; printf 'awake\\r\\n'"),
		Size(40, 5))
	start := time.Now()
	if err := term.WaitForStableScreen(50*time.Millisecond, WithTimeout(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Should have waited for the sleep + first paint + quiet window —
	// at minimum more than the 50ms quiet window alone.
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("WaitForStableScreen returned in %v, before first paint", elapsed)
	}
	if !contains(term.VisibleText(), "awake") {
		t.Fatalf("expected awake on screen, got %q", term.VisibleText())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
