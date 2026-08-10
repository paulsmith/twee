package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestDoScriptPathsResolveAgainstClientCwd(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "do-script-paths"
	daemonDir := t.TempDir()
	childDir := t.TempDir()
	clientDir := t.TempDir()

	start := exec.Command(bin, "start", "--name", name, "--dir", childDir, "--", "/bin/sh", "-c", "sleep 30")
	start.Dir = daemonDir
	start.Env = append(os.Environ(), env...)
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stop := exec.Command(bin, "stop", "--name", name)
		stop.Env = append(os.Environ(), env...)
		_ = stop.Run()
	})

	writePathScriptFixture(t, clientDir)
	writePathDecoys(t, daemonDir, childDir)
	cmd := exec.Command(bin, "do", "--name", name, "--script", "ops.json", "--emit", "results")
	cmd.Dir = clientDir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("do: %v\n%s", err, out)
	}
	assertScriptPathResults(t, out, clientDir)
	assertScriptArtifacts(t, clientDir, daemonDir, childDir)
}

func TestRunScriptPathsResolveAgainstClientCwd(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	clientDir := t.TempDir()
	childDir := t.TempDir()
	writePathScriptFixture(t, clientDir)
	writePathDecoys(t, childDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"run", "--script", "ops.json", "--dir", childDir, "--emit", "results",
		"--", "/bin/sh", "-c", "sleep 30",
	)
	cmd.Dir = clientDir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("run timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	assertScriptPathResults(t, out, clientDir)
	assertScriptArtifacts(t, clientDir, childDir)
}

func writePathScriptFixture(t *testing.T, clientDir string) {
	t.Helper()
	script := []byte(`[
		{"op":"screenshot","args":{"out":"shot.png","pixel_width":160,"pixel_height":100}},
		{"op":"trace_start","args":{"out":"session.twee"}},
		{"op":"trace_stop"},
		{"op":"diff","args":{"against":"expected.txt"}}
	]`)
	if err := os.WriteFile(filepath.Join(clientDir, "ops.json"), script, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientDir, "expected.txt"), []byte("client-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePathDecoys(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.WriteFile(filepath.Join(dir, "expected.txt"), []byte("wrong-directory"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertScriptPathResults(t *testing.T, out []byte, clientDir string) {
	t.Helper()
	var responses []rpc.Response
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var response rpc.Response
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("decode response %q: %v", scanner.Text(), err)
		}
		if !response.OK {
			t.Fatalf("operation failed: %s", scanner.Text())
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 4 {
		t.Fatalf("got %d responses, want 4:\n%s", len(responses), out)
	}

	data := func(index int) map[string]any {
		t.Helper()
		var value map[string]any
		if err := json.Unmarshal(responses[index].Data, &value); err != nil {
			t.Fatalf("decode response %d data: %v", index, err)
		}
		return value
	}
	if got, want := data(0)["out"], filepath.Join(clientDir, "shot.png"); got != want {
		t.Errorf("screenshot out = %v, want %q", got, want)
	}
	if got, want := data(1)["out"], filepath.Join(clientDir, "session.twee"); got != want {
		t.Errorf("trace start out = %v, want %q", got, want)
	}
	if got, want := data(2)["path"], filepath.Join(clientDir, "session.twee"); got != want {
		t.Errorf("trace stop path = %v, want %q", got, want)
	}
	if got := data(3)["expected"]; got != "client-sentinel" {
		t.Errorf("diff expected = %v, want client-sentinel", got)
	}
}

func assertScriptArtifacts(t *testing.T, clientDir string, otherDirs ...string) {
	t.Helper()
	for _, name := range []string{"shot.png", "session.twee"} {
		if _, err := os.Stat(filepath.Join(clientDir, name)); err != nil {
			t.Errorf("client artifact %s: %v", name, err)
		}
		for _, dir := range otherDirs {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("artifact %s written outside client cwd %s (stat err = %v)", name, dir, err)
			}
		}
	}
}
