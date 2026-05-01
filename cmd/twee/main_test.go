package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles cmd/twee into a temp dir once per test process.
func buildBinary(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "twee")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func TestVersion(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Errorf("expected non-empty version, got %q", out)
	}
}

func TestHelp(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !bytes.Contains(out, []byte("Usage: twee")) {
		t.Errorf("help output missing usage banner:\n%s", out)
	}
	if !bytes.Contains(out, []byte("record | trace | diff")) {
		t.Errorf("help output missing trace in state commands:\n%s", out)
	}
}

func TestTraceHelp(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "trace").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range [][]byte{
		[]byte("twee trace start"),
		[]byte("manifest.json"),
		[]byte("events.jsonl"),
		[]byte("screenshots/*.png"),
	} {
		if !bytes.Contains(out, want) {
			t.Errorf("trace help missing %q:\n%s", want, out)
		}
	}
}

func TestUnknownVerbExits2(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "bogus")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exit.ExitCode() != 2 {
		t.Errorf("exit code %d, want 2", exit.ExitCode())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("unknown subcommand")) {
		t.Errorf("stderr missing 'unknown subcommand':\n%s", stderr.String())
	}
}
