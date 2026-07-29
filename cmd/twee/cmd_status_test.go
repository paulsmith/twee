package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStatusAfterNaturalExitReadsTombstone pins down that "status" on a
// session whose child exited on its own (no socket left, no "twee stop"
// involved) reports its tombstone instead of NOT_FOUND: running:false,
// stopped:false, the exit code it actually returned, and no "signal"
// key at all (omitted, not empty-string) since it wasn't signaled.
func TestStatusAfterNaturalExitReadsTombstone(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-tomb-natural"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 0.2")
	mustOK(t, bin, env, "wait", "exit", "--name", name)

	tsPath := filepath.Join(stateDirOf(t, env), name+".exited")
	waitExists(t, tsPath, 3*time.Second)

	resp, raw, err := runCLI(t, bin, env, "status", "--name", name)
	if err != nil {
		t.Fatalf("status after natural exit: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("status after natural exit: %v", resp)
	}
	data, _ := resp["data"].(map[string]any)
	if data["name"] != name || data["running"] != false || data["stopped"] != false {
		t.Fatalf("data = %#v, want name/running:false/stopped:false", data)
	}
	if data["exit_code"] != float64(0) {
		t.Errorf("exit_code = %#v, want 0", data["exit_code"])
	}
	if _, present := data["signal"]; present {
		t.Errorf("data = %#v, want no \"signal\" key (not terminated by a signal)", data)
	}
	if _, present := data["stopped_at"]; !present {
		t.Errorf("data = %#v, missing stopped_at", data)
	}
	cmd, _ := data["command"].([]any)
	if len(cmd) == 0 {
		t.Errorf("data = %#v, missing command", data)
	}
}

// TestStatusAfterStopReadsTombstone pins down the other half: a session
// ended by an explicit "twee stop" reports stopped:true, exit_code:null,
// and the terminating signal's name (the default grace lets a plain
// "sleep" die from the SIGTERM itself, no escalation to SIGKILL needed).
//
// The tombstone isn't written until the daemon's own asynchronous
// teardown finishes — the same ~100ms-plus window "stop" and a natural
// exit already share for letting any still-connected client finish up —
// so this polls for the file rather than checking status immediately;
// a real caller doing the same back-to-back could transiently see
// NOT_FOUND in that window too (see the README's caveat on this).
func TestStatusAfterStopReadsTombstone(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-tomb-stopped"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	mustOK(t, bin, env, "stop", "--name", name)
	waitExists(t, filepath.Join(stateDirOf(t, env), name+".exited"), 3*time.Second)

	resp, raw, err := runCLI(t, bin, env, "status", "--name", name)
	if err != nil {
		t.Fatalf("status after stop: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("status after stop: %v", resp)
	}
	data, _ := resp["data"].(map[string]any)
	if data["running"] != false || data["stopped"] != true {
		t.Fatalf("data = %#v, want running:false stopped:true", data)
	}
	if data["exit_code"] != nil {
		t.Errorf("exit_code = %#v, want null (terminated by a signal)", data["exit_code"])
	}
	if data["signal"] != "SIGTERM" {
		t.Errorf("signal = %#v, want SIGTERM", data["signal"])
	}
}

// TestStatusNoSocketNoTombstoneIsNotFound pins down that a name with
// neither a live/stale socket nor a tombstone is still NOT_FOUND, with
// the same details.name/message-leads-with-name shape every dial-error
// NOT_FOUND now has.
func TestStatusNoSocketNoTombstoneIsNotFound(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "sm-status-never-existed"

	out := cliStdout(t, bin, env, "status", "--name", name)
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Name string `json:"name"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "NOT_FOUND" {
		t.Fatalf("status on never-started session = %s, want NOT_FOUND", out)
	}
	if resp.Error.Details.Name != name {
		t.Errorf("details.name = %q, want %q", resp.Error.Details.Name, name)
	}
}

// TestStatusCorruptTombstoneTreatedAsAbsent pins down that a tombstone
// file that fails to parse is treated the same as no tombstone at all
// (NOT_FOUND), not a crash or an INTERNAL error — readTombstone must
// tolerate whatever a half-written or manually-mangled file throws at
// it.
func TestStatusCorruptTombstoneTreatedAsAbsent(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	const name = "sm-tomb-corrupt"

	if err := os.WriteFile(filepath.Join(stateDir, name+".exited"), []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("plant corrupt tombstone: %v", err)
	}

	out := cliStdout(t, bin, env, "status", "--name", name)
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "NOT_FOUND" {
		t.Fatalf("status with corrupt tombstone = %s, want NOT_FOUND", out)
	}
}

// TestStartClearsOldTombstone pins down that a new "twee start" under a
// name with an old tombstone removes it, so a script that starts,
// finishes, and restarts under the same name never sees stale exit info
// bleed into the new session once that one has ended too.
func TestStartClearsOldTombstone(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	name := "sm-tomb-restart"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 0.2")
	mustOK(t, bin, env, "wait", "exit", "--name", name)
	tsPath := filepath.Join(stateDir, name+".exited")
	waitExists(t, tsPath, 3*time.Second)

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	waitGone(t, tsPath, 3*time.Second)

	statusOut, raw, err := runCLI(t, bin, env, "status", "--name", name)
	if err != nil {
		t.Fatalf("status after restart: %v\n%s", err, raw)
	}
	data, _ := statusOut["data"].(map[string]any)
	if data["running"] != true {
		t.Errorf("status after restart = %#v, want running:true (live session, not the old tombstone)", data)
	}
}

// TestLsDoesNotListTombstones pins down that "ls" (which enumerates
// "*.sock" entries) never surfaces a bare tombstone file as an entry —
// tombstones are a "status" concern only.
func TestLsDoesNotListTombstones(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)
	name := "sm-tomb-ls"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 0.2")
	mustOK(t, bin, env, "wait", "exit", "--name", name)
	waitExists(t, filepath.Join(stateDir, name+".exited"), 3*time.Second)

	resp, raw, err := runCLI(t, bin, env, "ls")
	if err != nil {
		t.Fatalf("ls: %v\n%s", err, raw)
	}
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data = %#v, want array", resp["data"])
	}
	for _, e := range data {
		entry, _ := e.(map[string]any)
		if entry["name"] == name {
			t.Fatalf("ls listed a tombstone-only session: %#v", entry)
		}
	}
}

// waitExists polls until path exists or the deadline passes.
func waitExists(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s still missing after %s", path, timeout)
}
