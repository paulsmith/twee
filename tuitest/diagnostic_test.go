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
		"type payload redacted",
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

func TestDiagnosticIncludesModes(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf '\\033[?1h\\033[?2004h\\033[>1u\\033[?1003h\\033[?1006hREADY'; sleep 30"), Size(40, 5))
	if err := term.WaitForText("READY"); err != nil {
		t.Fatal(err)
	}
	diagnostic := term.Diagnostic()
	for _, want := range []string{
		"application cursor: true",
		"bracketed paste: true",
		"kitty keyboard known: true",
		"kitty keyboard flags: 1",
		"mouse enabled: true",
		"mouse tracking: unknown",
		"mouse format: unknown",
		"mouse raw tracking: x10=false normal=false button=false any=true",
		"mouse raw format: utf8=false sgr=true",
	} {
		if !strings.Contains(diagnostic, want) {
			t.Errorf("diagnostic missing %q. full text:\n%s", want, diagnostic)
		}
	}
}

func TestRunAutoRecordsTraceToTempDir(t *testing.T) {
	term := Run(t, "/bin/sh", Args("-c", "printf 'recorded\\r\\n'"), Size(40, 5))
	if err := term.WaitForText("recorded"); err != nil {
		t.Fatal(err)
	}
	if term.TracePath() == "" {
		t.Fatal("expected auto tracePath to be set")
	}
	if !strings.Contains(term.TracePath(), "session.twee") {
		t.Errorf("tracePath = %q, want suffix session.twee", term.TracePath())
	}
}
