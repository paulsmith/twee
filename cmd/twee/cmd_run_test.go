package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRunArgsInterspersedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "script before command",
			args: []string{"-script", "ops.json", "vim"},
		},
		{
			name: "script after command",
			args: []string{"vim", "--script", "ops.json"},
		},
		{
			name: "equals form",
			args: []string{"vim", "--script=ops.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseRunArgs(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if opts.scriptPath != "ops.json" {
				t.Fatalf("scriptPath = %q, want ops.json", opts.scriptPath)
			}
			if len(opts.cmd) != 1 || opts.cmd[0] != "vim" {
				t.Fatalf("cmd = %#v, want [vim]", opts.cmd)
			}
		})
	}
}

func TestParseRunArgsRunFlagsAndCommandFlags(t *testing.T) {
	opts, err := parseRunArgs([]string{
		"-script", "ops.json",
		"-cols", "100",
		"cmd",
		"-child-flag",
		"--rows=40",
		"--emit", "results",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.scriptPath != "ops.json" || opts.cols != 100 || opts.rows != 40 || opts.emit != "results" {
		t.Fatalf("opts = %+v", opts)
	}
	want := []string{"cmd", "-child-flag"}
	if len(opts.cmd) != len(want) {
		t.Fatalf("cmd = %#v, want %#v", opts.cmd, want)
	}
	for i := range want {
		if opts.cmd[i] != want[i] {
			t.Fatalf("cmd = %#v, want %#v", opts.cmd, want)
		}
	}
}

func TestRunScriptFlagOrdersViaCLI(t *testing.T) {
	bin := buildBinary(t)
	script := filepath.Join(t.TempDir(), "ops.json")
	if err := os.WriteFile(script, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "script before command",
			args: []string{"run", "-script", script, "/bin/sh", "-c", "exit 0"},
		},
		{
			name: "script after command",
			args: []string{"run", "/bin/sh", "-c", "exit 0", "--script", script},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, tt.args...)
			cmd.Env = append(os.Environ(), testEnv(t)...)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("command hung: %v\n%s", tt.args, out)
			}
			if err != nil {
				t.Fatalf("%v: %v\n%s", tt.args, err, out)
			}
			var resp map[string]any
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("decode %s: %v", out, err)
			}
			if resp["ok"] != true {
				t.Fatalf("response = %s", out)
			}
		})
	}
}
