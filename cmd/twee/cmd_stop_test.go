package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStopAllEmptyStateDir pins down that "stop --all" against an empty
// state dir reports data:[] rather than null, matching "ls"'s empty-case
// shape, and exits 0.
func TestStopAllEmptyStateDir(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	resp, raw, err := runCLI(t, bin, env, "stop", "--all")
	if err != nil {
		t.Fatalf("stop --all: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("stop --all: %v", resp)
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) != 0 {
		t.Fatalf("data = %#v, want []", resp["data"])
	}
}

// TestStopAllStopsLiveAndCleansStale exercises the mixed case "stop
// --all" exists for: two live sessions and one left stale by a kill -9,
// all in the same state dir. Every session must be accounted for in one
// pass, and the state dir must end up empty.
func TestStopAllStopsLiveAndCleansStale(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	stateDir := stateDirOf(t, env)

	mustOK(t, bin, env, "start", "--name", "sm-all-a", "--", "/bin/sh", "-c", "sleep 30")
	mustOK(t, bin, env, "start", "--name", "sm-all-b", "--", "/bin/sh", "-c", "sleep 30")

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", "sm-all-stale", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, raw)
	}
	data, _ := startOut["data"].(map[string]any)
	pidF, _ := data["pid"].(float64)
	pid := int(pidF)
	if pid == 0 {
		t.Fatalf("start response missing pid: %v", startOut)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 %d: %v", pid, err)
	}
	waitProcessGone(t, pid, 3*time.Second)

	defer func() {
		exec.Command(bin, "stop", "--name", "sm-all-a").Run()
		exec.Command(bin, "stop", "--name", "sm-all-b").Run()
		exec.Command(bin, "stop", "--name", "sm-all-stale").Run()
	}()

	resp, raw, err := runCLI(t, bin, env, "stop", "--all")
	if err != nil {
		t.Fatalf("stop --all: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("stop --all: %v", resp)
	}
	list, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data = %#v, want array", resp["data"])
	}
	byName := map[string]map[string]any{}
	for _, e := range list {
		entry, _ := e.(map[string]any)
		if name, _ := entry["name"].(string); name != "" {
			byName[name] = entry
		}
	}
	if e := byName["sm-all-a"]; e["stopped"] != true {
		t.Errorf("sm-all-a entry = %#v, want stopped:true", e)
	}
	if e := byName["sm-all-b"]; e["stopped"] != true {
		t.Errorf("sm-all-b entry = %#v, want stopped:true", e)
	}
	if e := byName["sm-all-stale"]; e["stopped"] != false || e["stale_cleaned"] != true {
		t.Errorf("sm-all-stale entry = %#v, want stopped:false stale_cleaned:true", e)
	}

	remaining, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("state dir not empty after stop --all: %v", remaining)
	}
}

// TestStopAllMutuallyExclusiveWithName pins down that "--all" combined
// with either a local or a global "--name" is a usage error (exit 2, no
// JSON on stdout), not a silent precedence rule.
func TestStopAllMutuallyExclusiveWithName(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	for _, args := range [][]string{
		{"stop", "--all", "--name", "sm-excl"},
		{"--name", "sm-excl", "stop", "--all"},
	} {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%v: expected failure, got %v\n%s", args, err, out)
		}
		if exit.ExitCode() != 2 {
			t.Fatalf("%v: exit = %d, want 2\n%s", args, exit.ExitCode(), out)
		}
		if strings.Contains(string(out), `"ok"`) {
			t.Errorf("%v: expected plain usage error, got JSON: %s", args, out)
		}
	}
}

// TestStopGraceZeroKillsImmediately pins down "--grace 0" as documented:
// SIGKILL immediately, no SIGTERM grace window. The child traps SIGTERM
// so it would survive the default 250ms window; with --grace 0 it must
// be gone almost immediately instead.
func TestStopGraceZeroKillsImmediately(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-grace-zero"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", `trap "" TERM; sleep 30`)

	start := time.Now()
	stopOut, raw, err := runCLI(t, bin, env, "stop", "--name", name, "--grace", "0")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("stop --grace 0: %v\n%s", err, raw)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop --grace 0: %v", stopOut)
	}
	// Well under the default 250ms grace, which this child would
	// otherwise survive (it ignores SIGTERM).
	if elapsed > 200*time.Millisecond {
		t.Errorf("stop --grace 0 took %s, want well under the default 250ms grace", elapsed)
	}
}

// TestStopGraceNegativeIsInvalidArgument pins down that a negative grace
// is rejected rather than silently treated as zero or as "no override".
func TestStopGraceNegativeIsInvalidArgument(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-grace-neg"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	out := cliStdout(t, bin, env, "stop", "--name", name, "--grace=-1s")
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp.OK || resp.Error.Code != "INVALID_ARGUMENT" {
		t.Fatalf("stop --grace=-1s = %s, want INVALID_ARGUMENT", out)
	}
}

// TestStopGraceCustomWindowLetsChildExitOnItsOwn pins down that a
// generous --grace gives a cooperative child room to exit on its own
// after SIGTERM instead of always being killed. The child blocks on the
// "read" builtin rather than an external "sleep": most shells defer a
// trap until a foreground *child process* exits, so a trap set around
// "sleep 30" wouldn't fire until sleep itself finished (or was killed) —
// "read" blocks the shell's own syscall instead, so the trap runs the
// moment the signal arrives.
func TestStopGraceCustomWindowLetsChildExitOnItsOwn(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-grace-custom"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--",
		"/bin/sh", "-c", `trap 'exit 0' TERM; read x`)

	start := time.Now()
	stopOut, raw, err := runCLI(t, bin, env, "stop", "--name", name, "--grace", "2s")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("stop --grace 2s: %v\n%s", err, raw)
	}
	if stopOut["ok"] != true {
		t.Fatalf("stop --grace 2s: %v", stopOut)
	}
	// The child exits on its own as soon as the signal is delivered;
	// stop must not wait out the full 2s grace.
	if elapsed > 1*time.Second {
		t.Errorf("stop --grace 2s took %s, want it to return once the child exited on its own", elapsed)
	}
}
