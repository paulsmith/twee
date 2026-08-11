package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// menuFixtureBinary builds (or reuses) the fixtures/menu TUI used across
// the CLI e2e tests, writing it to <repo>/bin/menu like the tuitest
// package's own helper does, so repeated test runs don't rebuild it.
func menuFixtureBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	bin := filepath.Join(root, "bin", "menu")
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	cmd := exec.Command("go", "build", "-o", bin, "./fixtures/menu")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build menu fixture: %v\n%s", err, out)
	}
	return bin
}

func writeScript(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ops.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDoScriptAgainstNamedSession is the core "twee do" happy path: start
// a named session up front (as any other daemon verb would), then batch
// a wait/key/wait/key/wait script against it in a single "twee do"
// invocation instead of five separate CLI processes, and confirm the
// ops actually took effect on the running session.
func TestDoScriptAgainstNamedSession(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	menuBin := menuFixtureBinary(t)
	name := "do-menu"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", menuBin)

	script := writeScript(t, `[
		{"op":"wait_text","args":{"text":"Choose an option","timeout":"2s"}},
		{"op":"key","args":{"key":"Down"}},
		{"op":"wait_text","args":{"text":"> second","timeout":"2s"}},
		{"op":"key","args":{"key":"Enter"}},
		{"op":"wait_text","args":{"text":"selected: second","timeout":"2s"}}
	]`)

	cmd := exec.Command(bin, "do", "--name", name, "--script", script)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("do: %v\n%s", err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != true {
		t.Fatalf("response = %s", out)
	}
	data, _ := resp["data"].(map[string]any)
	if data["ops"] != float64(5) {
		t.Fatalf("data.ops = %v, want 5", data)
	}

	// The script's key presses must have actually reached the session:
	// confirm the effect, not just that the summary claims success.
	textOut, raw, err := runCLI(t, bin, env, "text", "--name", name)
	if err != nil {
		t.Fatalf("text: %v\n%s", err, raw)
	}
	textData, _ := textOut["data"].(map[string]any)
	text, _ := textData["text"].(string)
	if !strings.Contains(text, "selected: second") {
		t.Fatalf("viewport = %q, want it to contain %q", text, "selected: second")
	}
}

// TestDoStdinScript covers "--script -" (and the omitted-flag default),
// reading the op script from stdin instead of a file, against a plain
// long-lived child (no TUI behavior needed for this one).
func TestDoStdinScript(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "do-stdin"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	for _, args := range [][]string{
		{"do", "--name", name},
		{"do", "--name", name, "--script", "-"},
	} {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdin = bytes.NewBufferString(`[{"op":"status"}]`)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		var resp map[string]any
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("decode %s: %v", out, err)
		}
		if resp["ok"] != true {
			t.Fatalf("%v response = %s", args, out)
		}
	}
}

// TestDoFailingScriptReportsOpError checks that a script whose second op
// fails strict arg validation stops the script there and reports that
// op's error envelope — same as "twee run" — rather than continuing or
// silently swallowing the failure.
func TestDoFailingScriptReportsOpError(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "do-fail"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	script := writeScript(t, `[
		{"op":"status"},
		{"op":"wait_text","args":{"pattern":"nope"}}
	]`)

	cmd := exec.Command(bin, "do", "--name", name, "--script", script)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected non-zero exit, got %v\n%s", err, out)
	}
	if exit.ExitCode() != 1 {
		t.Fatalf("exit = %d, want 1\n%s", exit.ExitCode(), out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if resp["ok"] != false {
		t.Fatalf("response = %s", out)
	}
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("error = %#v, want INVALID_ARGUMENT", errObj)
	}
	if !strings.Contains(errObj["message"].(string), "pattern") {
		t.Fatalf("message = %q, want it to name the bad key", errObj["message"])
	}

	// The session itself must survive an op-script failure: "do" doesn't
	// tear anything down on error, it just reports and exits.
	mustOK(t, bin, env, "status", "--name", name)
}

// TestDoEmitResultsStreamsNDJSON checks --emit results' streaming shape:
// one JSON line per op, each carrying the op's index as "id", instead
// of the one-line summary envelope.
func TestDoEmitResultsStreamsNDJSON(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "do-emit"
	defer exec.Command(bin, "stop", "--name", name).Run()

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	script := writeScript(t, `[{"op":"status"},{"op":"status"}]`)

	cmd := exec.Command(bin, "do", "--name", name, "--script", script, "--emit", "results")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("do --emit results: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), out)
	}
	for i, line := range lines {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode line %d %q: %v", i, line, err)
		}
		wantID := string(rune('0' + i))
		if resp["id"] != wantID {
			t.Fatalf("line %d: id = %v, want %q", i, resp["id"], wantID)
		}
		if resp["ok"] != true {
			t.Fatalf("line %d response = %s", i, line)
		}
	}
}

func TestDoEmitResultsStreamContracts(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "do-emit-contract"
	t.Cleanup(func() { _ = exec.Command(bin, "stop", "--name", name).Run() })
	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	for _, tt := range []struct {
		name     string
		script   string
		exitCode int
		lastOK   bool
	}{
		{name: "success", script: `[{"op":"status"},{"op":"status"}]`, lastOK: true},
		{name: "terminal operation failure", script: `[{"op":"status"},{"op":"wait_text","args":{"pattern":"nope"}}]`, exitCode: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			script := writeScript(t, tt.script)
			result := runContractCLI(t, bin, env, "do", "--name", name, "--script", script, "--emit", "results")
			if result.exitCode != tt.exitCode {
				t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", result.exitCode, tt.exitCode, result.stdout, result.stderr)
			}
			if len(result.stderr) != 0 {
				t.Fatalf("stderr = %q, want empty", result.stderr)
			}
			lines := strings.Split(strings.TrimSpace(string(result.stdout)), "\n")
			if len(lines) != 2 {
				t.Fatalf("records = %d, want 2 (no summary)\n%s", len(lines), result.stdout)
			}
			for i, line := range lines {
				var response struct {
					ID string `json:"id"`
					OK bool   `json:"ok"`
				}
				if err := json.Unmarshal([]byte(line), &response); err != nil {
					t.Fatalf("record %d is not JSON: %v\n%s", i, err, line)
				}
				if response.ID != fmt.Sprint(i) {
					t.Fatalf("record %d id = %q", i, response.ID)
				}
				if i == len(lines)-1 && response.OK != tt.lastOK {
					t.Fatalf("terminal record ok = %v, want %v\n%s", response.OK, tt.lastOK, line)
				}
			}
		})
	}
}

// TestDoNoSessionIsNotFound pins down that "do" resolves and reports a
// missing session exactly like every other daemon verb (NOT_FOUND),
// including the degenerate case of an empty ops array — which would
// otherwise never dial the session at all and could mask a missing
// session behind a hollow success.
func TestDoNoSessionIsNotFound(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	for _, raw := range []string{`[]`, `[{"op":"status"}]`} {
		script := writeScript(t, raw)
		cmd := exec.Command(bin, "do", "--name", "does-not-exist", "--script", script)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit for script %s, got success:\n%s", raw, out)
		}
		var resp map[string]any
		if jsonErr := json.Unmarshal(out, &resp); jsonErr != nil {
			t.Fatalf("decode %s: %v", out, jsonErr)
		}
		errObj, _ := resp["error"].(map[string]any)
		if errObj["code"] != "NOT_FOUND" {
			t.Fatalf("script %s: error = %#v, want NOT_FOUND", raw, errObj)
		}
	}
}
