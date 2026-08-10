package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTokenScopedStopCannotStopReplacement(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "token-replacement"
	defer exec.Command(bin, "stop", "--name", name).Run()

	first := mustStartData(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	firstToken, _ := first["token"].(string)
	if firstToken == "" {
		t.Fatalf("first start response missing token: %#v", first)
	}
	mustOK(t, bin, env, "stop", "--name", name, "--token", firstToken)

	second := mustStartData(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	secondToken, _ := second["token"].(string)
	if secondToken == "" || secondToken == firstToken {
		t.Fatalf("replacement token = %q, first = %q", secondToken, firstToken)
	}

	out := cliStdout(t, bin, env, "stop", "--name", name, "--token", firstToken)
	if !strings.Contains(string(out), "FAILED_PRECONDITION") {
		t.Fatalf("stale token stop = %s, want FAILED_PRECONDITION", out)
	}
	status := mustStartData(t, bin, env, "status", "--name", name)
	if status["running"] != true {
		t.Fatalf("replacement status after stale stop = %#v, want running", status)
	}
	mustOK(t, bin, env, "stop", "--name", name, "--token", secondToken)
}

func TestStartTokenOutIsPrivateAndMatchesResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "token-file"
	defer exec.Command(bin, "stop", "--name", name).Run()

	path := filepath.Join(t.TempDir(), "owner.token")
	data := mustStartData(t, bin, env, "start", "--name", name, "--token-out", path, "--", "/bin/sh", "-c", "sleep 30")
	token, _ := data["token"].(string)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != token {
		t.Fatalf("token file = %q, response token = %q", raw, token)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token file mode = %04o, want 0600", got)
	}
	mustOK(t, bin, env, "stop", "--name", name, "--token", token)
}

func mustStartData(t *testing.T, bin string, env []string, args ...string) map[string]any {
	t.Helper()
	resp, raw, err := runCLI(t, bin, env, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, raw)
	}
	if resp["ok"] != true {
		t.Fatalf("%v: response = %#v", args, resp)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("%v: data = %#v", args, resp["data"])
	}
	return data
}

func TestFailedCollisionDoesNotCreateTokenFile(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "token-collision"
	defer exec.Command(bin, "stop", "--name", name).Run()
	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	path := filepath.Join(t.TempDir(), "owner.token")
	out := cliStdout(t, bin, env, "start", "--name", name, "--token-out", path, "--", "/bin/sh", "-c", "sleep 30")
	if !strings.Contains(string(out), "ALREADY_RUNNING") {
		t.Fatalf("collision = %s, want ALREADY_RUNNING", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed start created token file: %v", err)
	}
}

func TestExplicitEmptyTokenNeverFallsBackToNameOnlyStop(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	const name = "token-empty-trap"
	defer exec.Command(bin, "stop", "--name", name).Run()
	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")

	cmd := exec.Command(bin, "stop", "--name", name, "--token", "")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("empty token stop exit = %v, want usage exit 2\n%s", err, out)
	}
	if !strings.Contains(string(out), "token must not be empty") {
		t.Fatalf("empty token stop = %s, want safe rejection", out)
	}
	status := mustStartData(t, bin, env, "status", "--name", name)
	if status["running"] != true {
		t.Fatalf("empty token stopped current daemon: %#v", status)
	}
}
