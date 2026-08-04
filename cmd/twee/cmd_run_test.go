package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/play"
)

func TestParseRunArgsInterspersedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "script before boundary",
			args: []string{"--script", "ops.json", "--", "vim"},
		},
		{
			name: "equals form before boundary",
			args: []string{"--script=ops.json", "--", "vim"},
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
		"--script", "ops.json",
		"--cols", "100",
		"--rows=40",
		"--trace-out", "session.twee",
		"--emit", "results",
		"--",
		"cmd",
		"-child-flag",
	})
	if err != nil {
		t.Fatal(err)
	}
	// --trace-out is resolved against the client's cwd (see absOutPath)
	// so it travels over the wire unambiguously.
	wantTrace, err := filepath.Abs("session.twee")
	if err != nil {
		t.Fatal(err)
	}
	if opts.scriptPath != "ops.json" || opts.cols != 100 || opts.rows != 40 || opts.emit != "results" || opts.tracePath != wantTrace {
		t.Fatalf("opts = %+v, want tracePath %q", opts, wantTrace)
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

func TestParseRunNetworkCapture(t *testing.T) {
	opts, err := parseRunArgs([]string{"--trace-out", "session.twee", "--network-capture", "--publish-tcp", "127.0.0.1:8080=10.0.2.100:3000", "--", "server"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.networkCapture || len(opts.publishTCP) != 1 {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.publishTCP[0].Guest != "10.0.2.100:3000" {
		t.Fatalf("publication = %+v", opts.publishTCP[0])
	}
}

func TestParseRunNetworkCaptureRequiresTrace(t *testing.T) {
	if _, err := parseRunArgs([]string{"--network-capture", "--", "server"}); err == nil {
		t.Fatal("network capture without trace succeeded")
	}
}

func TestRunTraceOutWritesBundle(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "ops.json")
	tracePath := filepath.Join(dir, "session.twee")
	raw := []byte(`[
		{"op":"type","args":{"text":"abc\n"}},
		{"op":"wait_text","args":{"text":"abc","timeout":"2s"}}
	]`)
	if err := os.WriteFile(script, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"run",
		"--script", script,
		"--trace-out", tracePath,
		"--",
		"/bin/cat",
	)
	cmd.Env = append(os.Environ(), testEnv(t)...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("command hung: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != true {
		t.Fatalf("response = %s", out)
	}

	bundle, err := play.OpenBundle(tracePath)
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}
	if len(bundle.Manifest.Command) != 1 || bundle.Manifest.Command[0] != "/bin/cat" {
		t.Fatalf("manifest command = %#v, want [/bin/cat]", bundle.Manifest.Command)
	}
	if !eventsContain(bundle.Events, "input", "type", "", []byte("abc\n")) {
		t.Fatalf("trace missing typed input event: %#v", bundle.Events)
	}
	if !eventsContain(bundle.Events, "output", "", "", []byte("abc")) {
		t.Fatalf("trace missing output containing abc: %#v", bundle.Events)
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
			name: "script before boundary",
			args: []string{"run", "--script", script, "--", "/bin/sh", "-c", "exit 0"},
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

func TestRunRequiresExplicitBoundaryViaCLI(t *testing.T) {
	bin := buildBinary(t)
	script := filepath.Join(t.TempDir(), "ops.json")
	if err := os.WriteFile(script, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "--script", script, "/bin/echo", "ok")
	cmd.Env = append(os.Environ(), testEnv(t)...)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected missing-boundary failure, got %v\n%s", err, out)
	}
	if exit.ExitCode() != 2 {
		t.Fatalf("exit = %d, want 2\n%s", exit.ExitCode(), out)
	}
}
