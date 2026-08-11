package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var testBinary string

// TestMain builds the CLI once for the package's end-to-end tests. Individual
// tests still use their own temporary state directories through testEnv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "twee-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create CLI test directory:", err)
		os.Exit(1)
	}
	testBinary = filepath.Join(dir, "twee")
	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI test binary: %v\n%s", err, out)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "remove CLI test directory:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// buildBinary returns the CLI built by TestMain for this test process.
func buildBinary(t *testing.T) string {
	t.Helper()
	if testBinary == "" {
		t.Fatal("CLI test binary was not initialized")
	}
	return testBinary
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
	if !bytes.Contains(out, []byte("Export a .twee trace bundle to GIF, self-contained HTML, MP4, or WebM")) {
		t.Errorf("help output missing HTML export summary:\n%s", out)
	}
	if bytes.Contains(out, []byte(".md")) {
		t.Errorf("help output should not link to markdown docs:\n%s", out)
	}
	if bytes.Contains(out, []byte("  bundle  ")) {
		t.Errorf("help output still advertises removed bundle command:\n%s", out)
	}
	last := -1
	for _, verb := range []string{"cell", "click", "completion", "cursor", "diff", "drag", "export", "find", "help", "hover", "inspect", "key", "keys", "lines", "ls", "mode", "paste", "play", "region", "resize", "run", "screenshot", "scroll", "scrollback", "signal", "size", "sleep", "snapshot", "start", "status", "stop", "text", "title", "trace", "type", "version", "wait", "wrap"} {
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
	if bytes.Contains(out, []byte("screenshots")) {
		t.Errorf("top-level help should not mention screenshots:\n%s", out)
	}
}

func TestInputHelpDocumentsModeDependentBehavior(t *testing.T) {
	bin := buildBinary(t)
	keyHelp, err := exec.Command(bin, "help", "key").Output()
	if err != nil {
		t.Fatalf("help key: %v", err)
	}
	for _, want := range []string{"DECCKM", "Kitty keyboard protocol is active", "FAILED_PRECONDITION"} {
		if !bytes.Contains(keyHelp, []byte(want)) {
			t.Errorf("key help missing %q:\n%s", want, keyHelp)
		}
	}
	pasteHelp, err := exec.Command(bin, "help", "paste").Output()
	if err != nil {
		t.Fatalf("help paste: %v", err)
	}
	for _, want := range []string{"FAILED_PRECONDITION", "--force", "mode 2004", "writes no bytes", "ESC[200~", "twee type"} {
		if !bytes.Contains(pasteHelp, []byte(want)) {
			t.Errorf("paste help missing %q:\n%s", want, pasteHelp)
		}
	}
}

func TestBundleCommandRemoved(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "bundle", "info", "recording.twee")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %v", err)
	}
	if exit.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exit.ExitCode())
	}
	if !strings.Contains(stderr.String(), `unknown subcommand "bundle"`) {
		t.Fatalf("stderr missing removed-command error:\n%s", stderr.String())
	}
}

func TestRootHelpUsesLongOnlyHelp(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "--help").Output()
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !bytes.Contains(out, []byte("Usage: twee")) {
		t.Fatalf("--help output missing usage:\n%s", out)
	}

	cmd := exec.Command(bin, "-h")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected -h to fail with ExitError, got %v", err)
	}
	if exit.ExitCode() != 2 {
		t.Fatalf("-h exit code = %d, want 2", exit.ExitCode())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("short options are not supported")) {
		t.Fatalf("-h stderr = %s", stderr.String())
	}
}

func TestStaticHelpBeforeBoundaryOnly(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "wait", "text", "--help").Output()
	if err != nil {
		t.Fatalf("wait text --help: %v", err)
	}
	if !bytes.Contains(out, []byte("twee wait text --pattern TEXT")) {
		t.Fatalf("wait text help did not use static help:\n%s", out)
	}

	cmd := exec.Command(bin, "type", "--", "--help")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("type -- --help should reach command handling and fail without daemon, got %v", err)
	}
	if exit.ExitCode() == 2 {
		t.Fatalf("post-boundary --help was treated as usage help; stderr=%s", stderr.String())
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
	} {
		if !bytes.Contains(out, want) {
			t.Errorf("trace help missing %q:\n%s", want, out)
		}
	}
	if bytes.Contains(out, []byte("screenshots")) {
		t.Errorf("trace help still mentions screenshots:\n%s", out)
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

func TestRecordVerbRemoved(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "record")
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
