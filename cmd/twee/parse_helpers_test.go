package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestParseRunArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing boundary", []string{"/bin/echo"}, "missing --"},
		{"script missing", []string{"--script"}, "missing --"},
		{"script value missing", []string{"--script", "--", "/bin/echo"}, "missing value"},
		{"cols missing", []string{"--cols", "--", "/bin/echo"}, "missing value"},
		{"cols bad", []string{"--cols", "zero", "--", "/bin/echo"}, "positive integer"},
		{"cols zero", []string{"--cols=0", "--", "/bin/echo"}, "positive integer"},
		{"rows missing", []string{"--rows", "--", "/bin/echo"}, "missing value"},
		{"rows bad", []string{"--rows", "-1", "--", "/bin/echo"}, "short options"},
		{"dir missing", []string{"--dir", "--", "/bin/echo"}, "missing value"},
		{"emit missing", []string{"--emit", "--", "/bin/echo"}, "missing value"},
		{"trace missing", []string{"--trace-out", "--", "/bin/echo"}, "missing value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRunArgs(tt.args)
			if err == nil {
				t.Fatal("parseRunArgs unexpectedly succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseRunArgsDoubleDashAndHelpers(t *testing.T) {
	opts, err := parseRunArgs([]string{"--cols", "120", "--", "cmd", "--not-a-twee-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.cols != 120 || !opts.colsSet {
		t.Fatalf("cols = %d set=%v, want 120 true", opts.cols, opts.colsSet)
	}
	if got := strings.Join(opts.cmd, " "); got != "cmd --not-a-twee-flag" {
		t.Fatalf("cmd = %q", got)
	}

	if name, value, ok := splitFlagValue("--rows=40"); name != "--rows" || value != "40" || !ok {
		t.Fatalf("splitFlagValue = %q %q %v", name, value, ok)
	}
	if name, value, ok := splitFlagValue("cmd"); name != "cmd" || value != "" || ok {
		t.Fatalf("splitFlagValue cmd = %q %q %v", name, value, ok)
	}
	if value, next, err := flagValue("--x", "y", true, nil, 3); err != nil || value != "y" || next != 3 {
		t.Fatalf("flagValue inline = %q %d %v", value, next, err)
	}
	if value, next, err := flagValue("--x", "", false, []string{"--x", "y"}, 0); err != nil || value != "y" || next != 1 {
		t.Fatalf("flagValue next = %q %d %v", value, next, err)
	}
	if _, _, err := flagValue("--x", "", false, []string{"--x"}, 0); err == nil {
		t.Fatal("flagValue missing unexpectedly succeeded")
	}
}

func TestReadScriptAndLeadingResize(t *testing.T) {
	script := []byte(`[{"op":"text"}]`)
	path := t.TempDir() + "/ops.json"
	if err := os.WriteFile(path, script, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readScript(path)
	if err != nil {
		t.Fatalf("readScript: %v", err)
	}
	if string(got) != string(script) {
		t.Fatalf("script = %s, want %s", got, script)
	}

	raw, err := json.Marshal(rpc.ResizeArgs{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	resize, ok := leadingResize([]rpc.Request{{Op: rpc.OpResize, Args: raw}})
	if !ok || resize.Cols != 100 || resize.Rows != 30 {
		t.Fatalf("leadingResize = %+v, %v; want 100x30 true", resize, ok)
	}
	for _, ops := range [][]rpc.Request{
		nil,
		{{Op: rpc.OpText}},
		{{Op: rpc.OpResize, Args: json.RawMessage(`{`)}},
		{{Op: rpc.OpResize, Args: mustJSONMain(t, rpc.ResizeArgs{Cols: 0, Rows: 30})}},
	} {
		if got, ok := leadingResize(ops); ok {
			t.Fatalf("leadingResize(%#v) = %+v, true; want false", ops, got)
		}
	}
}

func TestParsePlayArgs(t *testing.T) {
	path, opts := parsePlayArgs([]string{
		"--speed=2.5",
		"--step",
		"--max-idle", "150ms",
		"--verbose",
		"trace.twee",
	})
	if path != "trace.twee" {
		t.Fatalf("path = %q, want trace.twee", path)
	}
	if opts.Speed != 2.5 || !opts.Step || opts.MaxIdle != 150*time.Millisecond || !opts.Verbose {
		t.Fatalf("opts = %+v", opts)
	}
	if got := parsePlaySpeed("1.25"); got != 1.25 {
		t.Fatalf("parsePlaySpeed = %v, want 1.25", got)
	}
	if got := parsePlayDuration("3s"); got != 3*time.Second {
		t.Fatalf("parsePlayDuration = %v, want 3s", got)
	}
}

func TestParseCodegenArgs(t *testing.T) {
	opts, err := parseCodegenArgs([]string{
		"--out", "ops.json",
		"--trace-out", "session.twee",
		"--cols", "90",
		"--rows", "20",
		"--dir", "/tmp",
		"--env", "A=B",
		"--no-waits",
		"--",
		"vim",
		"--clean",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.OutPath != "ops.json" || opts.TracePath != "session.twee" || opts.Cols != 90 || opts.Rows != 20 || opts.Dir != "/tmp" || !opts.NoWaits {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.Env["A"] != "B" {
		t.Fatalf("env = %#v, want A=B", opts.Env)
	}
	if got := strings.Join(opts.Command, " "); got != "vim --clean" {
		t.Fatalf("command = %q", got)
	}
}

func TestParseCodegenArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing command", []string{"--out", "ops.json"}, "missing --"},
		{"missing out", []string{"--", "vim"}, "missing --out"},
		{"out value", []string{"--out", "--", "vim"}, "missing value"},
		{"trace value", []string{"--trace-out", "--", "vim"}, "missing value"},
		{"cols value", []string{"--cols", "--", "vim"}, "missing value"},
		{"cols bad", []string{"--out", "ops.json", "--cols", "bad", "--", "vim"}, "positive integer"},
		{"rows value", []string{"--rows", "--", "vim"}, "missing value"},
		{"rows bad", []string{"--out", "ops.json", "--rows", "0", "--", "vim"}, "positive integer"},
		{"dir value", []string{"--dir", "--", "vim"}, "missing value"},
		{"env value", []string{"--env", "--", "vim"}, "missing value"},
		{"env bad", []string{"--out", "ops.json", "--env", "NOPE", "--", "vim"}, "bad --env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCodegenArgs(tt.args)
			if err == nil {
				t.Fatal("parseCodegenArgs unexpectedly succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestSplitKVAndMultiFlag(t *testing.T) {
	for _, tt := range []struct {
		in        string
		key, val  string
		wantFound bool
	}{
		{"A=B", "A", "B", true},
		{"A=", "A", "", true},
		{"=B", "", "B", true},
		{"NOPE", "", "", false},
	} {
		key, val, ok := splitKV(tt.in)
		if key != tt.key || val != tt.val || ok != tt.wantFound {
			t.Fatalf("splitKV(%q) = %q %q %v", tt.in, key, val, ok)
		}
	}

	var flags multiFlag
	if err := flags.Set("A=B"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("C=D"); err != nil {
		t.Fatal(err)
	}
	if got := flags.String(); !strings.Contains(got, "A=B") || !strings.Contains(got, "C=D") {
		t.Fatalf("String = %q", got)
	}
}

func mustJSONMain(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMissingValueHintsDashLeadingValues(t *testing.T) {
	var waitOpts struct {
		Pattern string `arg:"--pattern"`
		Timeout string `arg:"--timeout"`
	}
	err := parseArg("wait text", &waitOpts, []string{"--pattern", "-- INSERT --", "--timeout", "3s"})
	if err == nil || !strings.Contains(err.Error(), `--pattern=VALUE`) {
		t.Errorf("dash-leading --pattern value: err = %v, want --pattern=VALUE hint", err)
	}

	var bareOpts struct {
		Pattern string `arg:"--pattern"`
	}
	err = parseArg("wait text", &bareOpts, []string{"--pattern"})
	if err == nil || strings.Contains(err.Error(), "=VALUE") {
		t.Errorf("trailing bare --pattern: err = %v, want plain missing-value error", err)
	}

	err = requireSeparateValues([]string{"--env", "--literal"}, "--env")
	if err == nil || !strings.Contains(err.Error(), `--env=VALUE`) {
		t.Errorf("dash-leading --env value: err = %v, want --env=VALUE hint", err)
	}

	err = requireSeparateValues([]string{"--env"}, "--env")
	if err == nil || strings.Contains(err.Error(), "=VALUE") {
		t.Errorf("trailing bare --env: err = %v, want plain missing-value error", err)
	}
}
