package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runCLI executes ./bin/twee with args from the test process' env. It
// must be called only with TWEE_STATE_DIR set in the env, so daemons
// don't collide with the user's real ~/.local/state.
func runCLI(t *testing.T, bin string, env []string, args ...string) (map[string]any, []byte, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		// Capture stderr if the cmd failed.
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, exitErr.Stderr, err
		}
		return nil, nil, err
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		return nil, out, fmt.Errorf("decode %s: %w", out, err)
	}
	return got, out, nil
}

// testEnv returns the env block for an isolated CLI run.
func testEnv(t *testing.T) []string {
	t.Helper()
	stateDir := t.TempDir()
	ghostty := filepath.Join(repoRoot(t), "build", "_deps", "ghostty-src", "zig-out", "lib")
	return []string{
		"TWEE_STATE_DIR=" + stateDir,
		"DYLD_LIBRARY_PATH=" + ghostty,
		"LD_LIBRARY_PATH=" + ghostty,
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == "/dev/null" {
		t.Fatal("not in a Go module")
	}
	return filepath.Dir(gomod)
}

func TestMenuFixtureViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	root := repoRoot(t)
	menuBin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(menuBin); err != nil {
		t.Skipf("menu fixture not built (run 'make build'): %v", err)
	}
	defer exec.Command(bin, "stop", "--name", "menu-test").Run()

	mustOK(t, bin, env, "start", "--name", "menu-test", menuBin)
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "Choose an option")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Down")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "> second")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Enter")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "selected: second")
	mustOK(t, bin, env, "stop", "--name", "menu-test")
}

func mustOK(t *testing.T, bin string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != true {
		t.Fatalf("%v: %s", args, out)
	}
}

func TestStartStatusStopRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	defer exec.Command(bin, "stop", "--name", "rt").Run()

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", "rt", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, raw)
	}
	if startOut["ok"] != true {
		t.Fatalf("start: %v", startOut)
	}

	time.Sleep(100 * time.Millisecond)

	statusOut, raw2, err := runCLI(t, bin, env, "status", "--name", "rt")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, raw2)
	}
	if statusOut["ok"] != true {
		t.Fatalf("status: %v", statusOut)
	}
	data, _ := statusOut["data"].(map[string]any)
	if data["running"] != true {
		t.Errorf("expected running=true, got %v", data)
	}

	stopOut, raw3, err := runCLI(t, bin, env, "stop", "--name", "rt")
	if err != nil {
		t.Fatalf("stop: %v\n%s", err, raw3)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop: %v", stopOut)
	}

	// Status after stop should fail.
	cmd := exec.Command(bin, "status", "--name", "rt")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected non-zero exit after stop")
	}
	if !strings.Contains(string(out), "NOT_FOUND") && !strings.Contains(string(out), "no such file") {
		t.Errorf("expected NOT_FOUND after stop, got %s", out)
	}
}
