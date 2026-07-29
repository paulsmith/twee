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

	mustOK(t, bin, env, "start", "--name", "menu-test", "--", menuBin)
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "--pattern", "Choose an option")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Down")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "--pattern", "> second")
	mustOK(t, bin, env, "key", "--name", "menu-test", "Enter")
	mustOK(t, bin, env, "wait", "text", "--name", "menu-test", "--pattern", "selected: second")
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

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", "rt", "--", "/bin/sh", "-c", "sleep 30")
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

func TestStartReportsImmediateChildExitAndCleansSocket(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := envValue(t, env, "TWEE_STATE_DIR")
	name := "quick-exit"

	cmd := exec.Command(bin, "start", "--name", name, "--", "/usr/bin/false")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected /bin/false start to fail, got %v\n%s", err, out)
	}
	if exit.ExitCode() == 0 {
		t.Fatalf("exit code = 0, want non-zero\n%s", out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != false {
		t.Fatalf("response = %s", out)
	}
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != "CHILD_EXITED" {
		t.Fatalf("error = %#v, want CHILD_EXITED", errObj)
	}
	details, _ := errObj["details"].(map[string]any)
	if details["name"] != name || details["exit_code"] != float64(1) || details["socket_created"] != true {
		t.Fatalf("details = %#v", details)
	}
	if _, err := os.Stat(filepath.Join(stateDir, name+".sock")); !os.IsNotExist(err) {
		t.Fatalf("socket still exists or stat failed unexpectedly: %v", err)
	}

	defer exec.Command(bin, "stop", "--name", name).Run()
	startOut, raw, err := runCLI(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("restart after quick exit: %v\n%s", err, raw)
	}
	if startOut["ok"] != true {
		t.Fatalf("restart response = %s", raw)
	}
}

func TestSessionNamePrecedenceViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "session-precedence"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	mustOK(t, bin, env, "--name", name, "status")
	mustOK(t, bin, append(env, "TWEE_SESSION="+name), "status")
	mustOK(t, bin, env, "--name", "missing", "status", "--name", name)

	cmd := exec.Command(bin, "--name", name, "sleep", "1ms")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected global --name sleep failure, got %v\n%s", err, out)
	}
	if exit.ExitCode() != 2 {
		t.Fatalf("exit = %d, want 2\n%s", exit.ExitCode(), out)
	}
}

func TestScreenshotUsesPTYPixelSizeViaCLI(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "shot-pixels"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "printf 'hi\\r\\n'; sleep 30")
	mustOK(t, bin, env, "wait", "text", "--name", name, "--pattern", "hi")

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

// TestRelativeOutPathsResolveAgainstClientCwd pins down that a relative
// --out is resolved against the *client's* cwd, not the daemon's. The
// daemon here is started from one directory; screenshot and trace start
// are then invoked with a relative --out from a different directory, and
// the file must land there (not next to the daemon), with the response
// echoing the resolved absolute path.
func TestRelativeOutPathsResolveAgainstClientCwd(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "relout"
	defer exec.Command(bin, "stop", "--name", name).Run()

	daemonDir := t.TempDir()
	clientDir := t.TempDir()

	startCmd := exec.Command(bin, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	startCmd.Dir = daemonDir
	startCmd.Env = append(os.Environ(), env...)
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}

	runIn := func(dir string, args ...string) map[string]any {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
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
		return resp
	}

	wantShot := filepath.Join(clientDir, "shot.png")
	shotResp := runIn(clientDir, "screenshot", "--name", name, "--out", "shot.png")
	shotData, _ := shotResp["data"].(map[string]any)
	if shotData["out"] != wantShot {
		t.Fatalf("screenshot data.out = %v, want %q", shotData["out"], wantShot)
	}
	if _, err := os.Stat(wantShot); err != nil {
		t.Errorf("screenshot not written to client cwd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(daemonDir, "shot.png")); !os.IsNotExist(err) {
		t.Errorf("screenshot incorrectly written to daemon cwd (stat err = %v)", err)
	}

	wantTrace := filepath.Join(clientDir, "session.twee")
	traceResp := runIn(clientDir, "trace", "start", "--name", name, "--out", "session.twee")
	traceData, _ := traceResp["data"].(map[string]any)
	if traceData["out"] != wantTrace {
		t.Fatalf("trace start data.out = %v, want %q", traceData["out"], wantTrace)
	}
	stopResp := runIn(clientDir, "trace", "stop", "--name", name)
	stopData, _ := stopResp["data"].(map[string]any)
	if stopData["path"] != wantTrace {
		t.Fatalf("trace stop data.path = %v, want %q", stopData["path"], wantTrace)
	}
	if _, err := os.Stat(wantTrace); err != nil {
		t.Errorf("trace bundle not written to client cwd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(daemonDir, "session.twee")); !os.IsNotExist(err) {
		t.Errorf("trace bundle incorrectly written to daemon cwd (stat err = %v)", err)
	}

	// diff --against is an input path, not an output path, but it's read
	// by the daemon the same way: a relative path must resolve against
	// the client's cwd, not the daemon's. Plant a decoy file in the
	// daemon's cwd with different content so a wrong resolution is
	// caught by content, not just by success/failure.
	if err := os.WriteFile(filepath.Join(clientDir, "expected.txt"), []byte("sentinel-value"), 0o644); err != nil {
		t.Fatalf("write client expected.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(daemonDir, "expected.txt"), []byte("wrong-file"), 0o644); err != nil {
		t.Fatalf("write daemon-dir decoy expected.txt: %v", err)
	}
	diffResp := runIn(clientDir, "diff", "--name", name, "--against", "expected.txt")
	diffData, _ := diffResp["data"].(map[string]any)
	if diffData["expected"] != "sentinel-value" {
		t.Fatalf("diff data.expected = %v, want %q (read from client cwd)", diffData["expected"], "sentinel-value")
	}
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	t.Fatalf("env missing %s", key)
	return ""
}
