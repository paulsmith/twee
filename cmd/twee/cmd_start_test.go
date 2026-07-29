package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestStartForceReplacesLiveSession pins down "start --force"'s primary
// case: a live collision is stopped instead of reported as
// ALREADY_RUNNING, the new daemon comes up under the same name, and the
// response says so via "replaced":true.
func TestStartForceReplacesLiveSession(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-force-live"
	defer exec.Command(bin, "stop", "--name", name).Run()

	firstOut, raw, err := runCLI(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("first start: %v\n%s", err, raw)
	}
	firstData, _ := firstOut["data"].(map[string]any)
	firstPidF, _ := firstData["pid"].(float64)
	firstPid := int(firstPidF)
	if firstPid == 0 {
		t.Fatalf("first start response missing pid: %v", firstOut)
	}

	secondOut, raw, err := runCLI(t, bin, env, "start", "--name", name, "--force", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start --force: %v\n%s", err, raw)
	}
	if secondOut["ok"] != true {
		t.Fatalf("start --force: %v", secondOut)
	}
	secondData, _ := secondOut["data"].(map[string]any)
	if secondData["replaced"] != true {
		t.Errorf("data = %#v, want replaced:true", secondData)
	}
	secondPidF, _ := secondData["pid"].(float64)
	if int(secondPidF) == firstPid {
		t.Errorf("second start reused the same pid %d, want a fresh daemon", firstPid)
	}

	// The old daemon must actually be gone, not merely disowned.
	waitProcessGone(t, firstPid, 3*time.Second)

	// And the new one must be answering under the same name.
	statusOut, raw, err := runCLI(t, bin, env, "status", "--name", name)
	if err != nil {
		t.Fatalf("status after --force: %v\n%s", err, raw)
	}
	statusData, _ := statusOut["data"].(map[string]any)
	if statusData["running"] != true {
		t.Errorf("status after --force = %#v, want running:true", statusData)
	}
}

// TestStartForceNoExistingSessionOmitsReplaced pins down that --force
// against a name with nothing running behaves like a plain start:
// "replaced" is absent (not false) since omitempty hides it, and no
// error surfaces from the no-op pre-stop step.
func TestStartForceNoExistingSessionOmitsReplaced(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-force-none"
	defer exec.Command(bin, "stop", "--name", name).Run()

	out, raw, err := runCLI(t, bin, env, "start", "--name", name, "--force", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start --force: %v\n%s", err, raw)
	}
	if out["ok"] != true {
		t.Fatalf("start --force: %v", out)
	}
	data, _ := out["data"].(map[string]any)
	if _, present := data["replaced"]; present {
		t.Errorf("data = %#v, want no \"replaced\" key at all", data)
	}
}

// TestStartForceOverStaleSessionOmitsReplaced pins down that --force
// against a stale (kill -9'd) session relies on start's existing
// stale-recovery path, unchanged: it succeeds, but doesn't claim
// "replaced" since nothing live was actually stopped.
func TestStartForceOverStaleSessionOmitsReplaced(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-force-stale"
	defer exec.Command(bin, "stop", "--name", name).Run()

	startOut, raw, err := runCLI(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
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

	out, raw, err := runCLI(t, bin, env, "start", "--name", name, "--force", "--", "/bin/sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("start --force over stale session: %v\n%s", err, raw)
	}
	if out["ok"] != true {
		t.Fatalf("start --force over stale session: %v", out)
	}
	outData, _ := out["data"].(map[string]any)
	if _, present := outData["replaced"]; present {
		t.Errorf("data = %#v, want no \"replaced\" key (stale recovery, not a live replacement)", outData)
	}
}

// TestStartWithoutForceStillReportsAlreadyRunning pins down that the
// default (no --force) behavior is unchanged: a live collision still
// fails with ALREADY_RUNNING rather than silently replacing anything.
func TestStartWithoutForceStillReportsAlreadyRunning(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "sm-noforce"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	cmd := exec.Command(bin, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("second start without --force succeeded: %s", out)
	}
	if !strings.Contains(string(out), "ALREADY_RUNNING") {
		t.Fatalf("second start without --force = %s, want ALREADY_RUNNING", out)
	}
}
