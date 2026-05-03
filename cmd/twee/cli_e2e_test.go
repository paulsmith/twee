package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
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

func runCLIOnPTY(t *testing.T, bin string, env []string, ws *pty.Winsize, args ...string) (map[string]any, []byte, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, nil, err
	}
	defer ptmx.Close()

	out, readErr := io.ReadAll(ptmx)
	waitErr := cmd.Wait()
	if readErr != nil && !errors.Is(readErr, syscall.EIO) {
		return nil, out, readErr
	}
	if waitErr != nil {
		return nil, out, waitErr
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out), &got); err != nil {
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

func TestScreenshotUsesPTYPixelSizeViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "shot-pixels"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "/bin/sh", "-c", "printf 'hi\\r\\n'; sleep 30")
	mustOK(t, bin, env, "wait", "text", "--name", name, "hi")

	outPath := filepath.Join(t.TempDir(), "screen.png")
	resp, raw, err := runCLIOnPTY(t, bin, env, &pty.Winsize{
		Rows: 24,
		Cols: 80,
		X:    333,
		Y:    222,
	}, "screenshot", "--name", name, "--out", outPath)
	if err != nil {
		t.Fatalf("screenshot: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("screenshot: %v", resp)
	}
	data, _ := resp["data"].(map[string]any)
	if data["width"] != float64(333) || data["height"] != float64(222) {
		t.Fatalf("response size = %v, want 333x222", data)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open screenshot: %v", err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}
	if cfg.Width != 333 || cfg.Height != 222 {
		t.Fatalf("png size = %dx%d, want 333x222", cfg.Width, cfg.Height)
	}
}
