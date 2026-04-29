package tuitest

import (
	"strings"
	"testing"
	"time"
)

func TestDiagnosticIncludesInputsAndExitStatus(t *testing.T) {
	term := Run(t, "/bin/sh",
		Args("-c", "printf 'hello\\r\\n'; read line; printf 'echo: %s\\r\\n' \"$line\""),
		Size(40, 5))
	if err := term.WaitForText("hello"); err != nil {
		t.Fatal(err)
	}
	if err := term.Type("world"); err != nil {
		t.Fatal(err)
	}
	if err := term.Key(Enter); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitForText("echo: world"); err != nil {
		t.Fatal(err)
	}
	// Wait for the child to exit so the diagnostic reports a code.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-term.ExitedCh():
			goto exited
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
exited:
	d := term.Diagnostic()
	for _, want := range []string{
		`Type "world"`,
		"Key Enter",
		"exit status: 0",
		"hello",
		"echo: world",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("diagnostic missing %q. full text:\n%s", want, d)
		}
	}
}

func TestRunAutoRecordsToTempDir(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'recorded\\r\\n'"), Size(40, 5))
	if err := term.WaitForText("recorded"); err != nil {
		t.Fatal(err)
	}
	if term.RecordPath() == "" {
		t.Fatal("expected auto recordPath to be set")
	}
	if !strings.Contains(term.RecordPath(), "session.jsonl") {
		t.Errorf("recordPath = %q, want suffix session.jsonl", term.RecordPath())
	}
}
