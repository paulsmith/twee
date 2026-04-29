//go:build flake
// +build flake

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFlake200Menu(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	root := repoRoot(t)
	menuBin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(menuBin); err != nil {
		t.Skipf("menu fixture not built: %v", err)
	}

	dir := t.TempDir()
	script := []map[string]any{
		{"op": "wait_text", "args": map[string]any{"text": "Choose an option", "timeout": "5s"}},
		{"op": "key", "args": map[string]any{"key": "Down"}},
		{"op": "wait_text", "args": map[string]any{"text": "> second", "timeout": "5s"}},
		{"op": "key", "args": map[string]any{"key": "Enter"}},
		{"op": "wait_text", "args": map[string]any{"text": "selected: second", "timeout": "5s"}},
	}
	b, _ := json.Marshal(script)
	scriptPath := filepath.Join(dir, "ops.json")
	if err := os.WriteFile(scriptPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 200; i++ {
		cmd := exec.Command(bin, "run", "--script", scriptPath, menuBin)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("iter %d: %v\n%s", i, err, out)
		}
	}
}
