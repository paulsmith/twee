package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	defer exec.Command(bin, "stop", "--name", sessionName).Run()

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
