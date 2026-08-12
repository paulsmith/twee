package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestLsEmptyReturnsArrayNotNull pins down that an empty "twee ls"
// reports data as [] rather than null, matching the non-empty case's
// shape (an array) instead of switching shape based on cardinality.
func TestLsEmptyReturnsArrayNotNull(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	cmd := exec.Command(bin, "ls")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ls: %v\n%s", err, out)
	}
	var resp struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if !resp.OK {
		t.Fatalf("ls failed: %s", out)
	}
	if string(resp.Data) != "[]" {
		t.Errorf("data = %s, want []", resp.Data)
	}
}

func TestLsListsRunningSessions(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "ls-running"
	defer func() { _ = exec.Command(bin, "stop", "--name", name).Run() }()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	resp, raw, err := runCLI(t, bin, env, "ls")
	if err != nil {
		t.Fatalf("ls: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("ls: %v", resp)
	}
	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data = %#v, want array", resp["data"])
	}
	found := false
	for _, e := range data {
		entry, _ := e.(map[string]any)
		if entry["name"] == name {
			found = true
			if entry["running"] != true {
				t.Errorf("entry for %s = %#v, want running:true", name, entry)
			}
		}
	}
	if !found {
		t.Fatalf("ls data %#v missing entry for %q", data, name)
	}
}

// TestLsListsStaleSession reproduces a daemon killed with SIGKILL: its
// socket and lock file linger on disk with nothing listening. "ls" must
// report it as a stale entry instead of silently omitting it.
func TestLsListsStaleSession(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "ls-stale"
	defer func() { _ = exec.Command(bin, "stop", "--name", name).Run() }()

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

	resp, raw, err := runCLI(t, bin, env, "ls")
	if err != nil {
		t.Fatalf("ls: %v\n%s", err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("ls: %v", resp)
	}
	list, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data = %#v, want array", resp["data"])
	}
	found := false
	for _, e := range list {
		entry, _ := e.(map[string]any)
		if entry["name"] == name {
			found = true
			if entry["running"] != false || entry["stale"] != true {
				t.Errorf("stale entry for %s = %#v, want running:false stale:true", name, entry)
			}
		}
	}
	if !found {
		t.Fatalf("ls data %#v missing stale entry for %q", list, name)
	}
}
