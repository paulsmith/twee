package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

// TestUnattendedAgentWorkflow exercises the cross-phase automation contract:
// token-scoped ownership, client-relative script artifacts, discoverable JSON
// metadata, atomic pattern clicking, and unconditional scoped cleanup.
func TestUnattendedAgentWorkflow(t *testing.T) {
	bin, env, dir := buildBinary(t), testEnv(t), t.TempDir()
	name := "unattended-agent"
	if err := os.Mkdir(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		return cmd.CombinedOutput()
	}
	var token string
	t.Cleanup(func() {
		if token != "" {
			_, _ = run("stop", "--name", name, "--token", token)
		}
	})

	startOut, err := run("start", "--name", name, "--token-out", "owner.token", "--",
		"/bin/sh", "-c", "printf '\\033[?1003h\\033[?1006hREADY Submit'; sleep 30")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, startOut)
	}
	var startResp struct {
		OK   bool `json:"ok"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(startOut, &startResp); err != nil || !startResp.OK || startResp.Data.Token == "" {
		t.Fatalf("start response: %v\n%s", err, startOut)
	}
	token = startResp.Data.Token
	owner, err := os.ReadFile(filepath.Join(dir, "owner.token"))
	if err != nil || strings.TrimSpace(string(owner)) != token {
		t.Fatalf("owner token: %v %q", err, owner)
	}

	script := `[
		{"op":"wait_text","args":{"text":"READY Submit","timeout":"2s"}},
		{"op":"trace_start","args":{"out":"artifacts/session.twee"}},
		{"op":"find_click","args":{"pattern":"Submit","require":"one"}},
		{"op":"trace_stop"}
	]`
	if err := os.WriteFile(filepath.Join(dir, "ops.json"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	doOut, err := run("do", "--name", name, "--script", "ops.json", "--emit", "results")
	if err != nil {
		t.Fatalf("do: %v\n%s", err, doOut)
	}
	lines := strings.Split(strings.TrimSpace(string(doOut)), "\n")
	if len(lines) != 4 {
		t.Fatalf("result records = %d\n%s", len(lines), doOut)
	}
	var clickResp struct {
		OK   bool              `json:"ok"`
		Data rpc.FindClickData `json:"data"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &clickResp); err != nil || !clickResp.OK || clickResp.Data.Selection != "exactly_one" {
		t.Fatalf("click response: %v %+v", err, clickResp)
	}
	tracePath := filepath.Join(dir, "artifacts", "session.twee")
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("relative trace: %v", err)
	}

	inspectOut, err := run("--machine", "inspect", "artifacts/session.twee")
	if err != nil {
		t.Fatalf("inspect: %v\n%s", err, inspectOut)
	}
	var inspect struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(inspectOut, &inspect); err != nil || !inspect.OK {
		t.Fatalf("machine inspect: %v\n%s", err, inspectOut)
	}
	helpOut, err := run("help", "click", "--format", "json")
	if err != nil || !json.Valid(helpOut) || !strings.Contains(string(helpOut), `--pattern`) {
		t.Fatalf("JSON click metadata: %v\n%s", err, helpOut)
	}

	stopOut, err := run("stop", "--name", name, "--token", token)
	if err != nil {
		t.Fatalf("token stop: %v\n%s", err, stopOut)
	}
	token = ""
}
