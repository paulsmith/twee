package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitProcessGone polls until pid no longer exists (kill(pid, 0) fails
// with ESRCH) or the deadline passes.
func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d still alive after %s", pid, timeout)
}

func stateDirOf(t *testing.T, env []string) string {
	t.Helper()
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "TWEE_STATE_DIR="); ok {
			return v
		}
	}
	t.Fatal("no TWEE_STATE_DIR in test env")
	return ""
}

// waitGone polls until path disappears or the deadline passes.
func waitGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s still exists after %s", path, timeout)
}

func TestLockRemovedOnNaturalExit(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const sessionName = "lock-natural"
	defer func() { _ = exec.Command(bin, "stop", "--name", sessionName).Run() }()

	mustOK(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 0.3")
	mustOK(t, bin, env, "wait", "exit", "--name", sessionName)

	// The daemon tears down shortly after the child exits; both its
	// socket and its lock file must go with it.
	waitGone(t, filepath.Join(stateDir, sessionName+".sock"), 3*time.Second)
	waitGone(t, filepath.Join(stateDir, sessionName+".lock"), 3*time.Second)
}

func TestLockRemovedOnQuickExit(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const sessionName = "lock-quick"

	if out := cliStdout(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "exit 3"); !strings.Contains(string(out), "CHILD_EXITED") {
		t.Fatalf("start envelope = %s, want CHILD_EXITED", out)
	}
	if _, err := os.Stat(filepath.Join(stateDir, sessionName+".lock")); !os.IsNotExist(err) {
		t.Errorf("lock file remains after quick-exit start: stat err = %v", err)
	}
}

func TestLockRemovedOnSpawnFailure(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const sessionName = "lock-noexec"

	if out := cliStdout(t, bin, env, "start", "--name", sessionName, "--", "/nonexistent-binary-xyz"); !strings.Contains(string(out), "no such file") {
		t.Fatalf("start envelope = %s, want spawn failure", out)
	}
	if _, err := os.Stat(filepath.Join(stateDir, sessionName+".lock")); !os.IsNotExist(err) {
		t.Errorf("lock file remains after failed spawn: stat err = %v", err)
	}
}

func TestStopCleansStaleLock(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const sessionName = "lock-stale"

	lock := filepath.Join(stateDir, sessionName+".lock")
	if err := os.WriteFile(lock, []byte("999999\n"), 0o600); err != nil {
		t.Fatalf("plant stale lock: %v", err)
	}
	out := cliStdout(t, bin, env, "stop", "--name", sessionName)
	if !strings.Contains(string(out), "NOT_FOUND") {
		t.Errorf("stop on stale session = %s, want NOT_FOUND", out)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("stale lock remains after stop: stat err = %v", err)
	}
}

// TestStopCleansTrulyStaleSocket reproduces a daemon killed with SIGKILL:
// unlike a graceful "stop" or a natural child exit, nothing removes the
// socket or lock file. A later "stop" must detect the now-unreachable
// (but still present) socket, clean up both files, and report success
// rather than NOT_FOUND.
func TestStopCleansTrulyStaleSocket(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const sessionName = "kill9-stale"

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, raw)
	}
	data, _ := startOut["data"].(map[string]any)
	pidF, ok := data["pid"].(float64)
	if !ok || pidF == 0 {
		t.Fatalf("start response missing pid: %v", startOut)
	}
	pid := int(pidF)

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 %d: %v", pid, err)
	}
	waitProcessGone(t, pid, 3*time.Second)

	sock := filepath.Join(stateDir, sessionName+".sock")
	lock := filepath.Join(stateDir, sessionName+".lock")
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket file should still be on disk after kill -9: %v", err)
	}

	stopOut, raw, err := runCLI(t, bin, env, "stop", "--name", sessionName)
	if err != nil {
		t.Fatalf("stop after kill -9: %v\n%s", err, raw)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop after kill -9 = %s, want ok:true", raw)
	}
	stopData, _ := stopOut["data"].(map[string]any)
	if stopData["stopped"] != false || stopData["stale_cleaned"] != true {
		t.Fatalf("stop data = %#v, want stopped:false stale_cleaned:true", stopData)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file remains after stale cleanup: stat err = %v", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("lock file remains after stale cleanup: stat err = %v", err)
	}
}

// TestStopOnMissingSessionStillNotFound pins down that a name with no
// socket file at all (never started, or already fully cleaned up) is
// still a plain NOT_FOUND, not confused with the stale-cleanup path. It
// also pins down that a NOT_FOUND from a failed dial carries
// details.name and a message leading with the session name rather than
// the socket path — every daemon-targeting verb shares this via
// dialErrorDetails, "stop" is just a convenient one to exercise it on.
func TestStopOnMissingSessionStillNotFound(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "never-existed"

	out := cliStdout(t, bin, env, "stop", "--name", name)
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				Name string `json:"name"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "NOT_FOUND" {
		t.Fatalf("stop on never-started session = %s, want NOT_FOUND", out)
	}
	if resp.Error.Details.Name != name {
		t.Errorf("details.name = %q, want %q", resp.Error.Details.Name, name)
	}
	if !strings.HasPrefix(resp.Error.Message, `session "`+name+`"`) {
		t.Errorf("message %q does not lead with the session name", resp.Error.Message)
	}
}

func TestStartNameCollisionReportsAlreadyRunning(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const sessionName = "lock-collide"
	defer func() { _ = exec.Command(bin, "stop", "--name", sessionName).Run() }()

	mustOK(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 30")
	out := cliStdout(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 30")
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode envelope %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "ALREADY_RUNNING" {
		t.Fatalf("second start envelope = %s, want ALREADY_RUNNING error", out)
	}
	if !strings.Contains(resp.Error.Message, sessionName) {
		t.Errorf("collision message %q does not name the session", resp.Error.Message)
	}
}

func TestDaemonRetainsSessionLockUnderGCPressure(t *testing.T) {
	bin := buildBinary(t)
	env := append(testEnv(t), "GOGC=1")
	const sessionName = "lock-gc-pressure"
	defer func() { _ = exec.Command(bin, "stop", "--name", sessionName).Run() }()

	mustOK(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 30")
	// Each request allocates and GOGC=1 forces frequent collections in the
	// daemon. The inherited lock descriptor must remain reachable throughout.
	for range 50 {
		mustOK(t, bin, env, "status", "--name", sessionName)
	}

	out := cliStdout(t, bin, env, "start", "--name", sessionName, "--", "/bin/sh", "-c", "sleep 30")
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode envelope %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "ALREADY_RUNNING" {
		t.Fatalf("second start after GC pressure = %s, want ALREADY_RUNNING", out)
	}
}
