package tuitest

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunHelloWorld(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'hello\\r\\nworld\\r\\n'"), Size(40, 5))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text := term.VisibleText()
		if strings.Contains(text, "hello") && strings.Contains(text, "world") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not see expected output:\n%s", term.Diagnostic())
}

func TestCloseTerminatesProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	term, err := Start(ctx, Command("/bin/sh", "-c", "sleep 30"), Size(40, 5))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := term.Close(); err != nil {
		t.Logf("Close: %v", err) // some EIO is expected
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %v, expected <3s", elapsed)
	}
}
