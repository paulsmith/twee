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
	if bytes.Contains(out, []byte(".md")) {
		t.Errorf("help output should not link to markdown docs:\n%s", out)
	}
	last := -1
	for _, verb := range []string{"cell", "codegen", "completion", "cursor", "diff", "find", "help", "key", "keys", "lines", "ls", "mode", "paste", "play", "record", "region", "resize", "run", "screenshot", "scrollback", "signal", "size", "sleep", "snapshot", "start", "status", "stop", "text", "title", "trace", "type", "version", "wait"} {
		needle := []byte("  " + verb + "  ")
		idx := bytes.Index(out, needle)
		if idx < 0 {
			t.Errorf("help output missing command %q:\n%s", verb, out)
			continue
		}
		if idx < last {
			t.Errorf("help output command %q is out of order:\n%s", verb, out)
		}
		last = idx
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

func TestRunHelpDoesNotLinkMarkdownSpec(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "run").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if bytes.Contains(out, []byte(".md")) {
		t.Errorf("run help should not link to markdown docs:\n%s", out)
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
