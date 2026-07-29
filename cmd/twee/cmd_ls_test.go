package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
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
	defer exec.Command(bin, "stop", "--name", name).Run()

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
