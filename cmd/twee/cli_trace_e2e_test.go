package main

import (
	"archive/zip"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestVimTraceEndToEnd drives a real vim instance through the twee CLI:
// start the daemon, start a trace, send some keys, stop the trace, stop
// the daemon, then read the .twee bundle back and check it captured the
// session.
func TestVimTraceEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("vim"); err != nil {
		t.Skip("vim not installed")
	}
	bin := buildBinary(t)
	env := testEnv(t)

	tmp := t.TempDir()
	bufFile := filepath.Join(tmp, "scratch.txt")
	tracePath := filepath.Join(tmp, "session.twee")
	const sessionName = "vim-trace"
	const typed = "hello world from twee"

	defer exec.Command(bin, "stop", "--name", sessionName).Run()

	mustOK(t, bin, env, "start",
		"--name", sessionName,
		"--cols", "80", "--rows", "24",
		"--env", "TERM=xterm-256color",
		"--",
		"vim", "-u", "NONE", "-N", "-n", bufFile,
	)

	// Vim's empty-buffer screen has tilde markers down the left edge.
	mustOK(t, bin, env, "wait", "text", "--name", sessionName, "--pattern", "~")

	mustOK(t, bin, env, "trace", "start",
		"--name", sessionName,
		"--out", tracePath,
	)

	mustOK(t, bin, env, "type", "--name", sessionName, "--", "i")
	mustOK(t, bin, env, "type", "--name", sessionName, "--", typed)
	mustOK(t, bin, env, "key", "--name", sessionName, "Escape")
	mustOK(t, bin, env, "wait", "text", "--name", sessionName, "--pattern", typed)

	mustOK(t, bin, env, "trace", "stop", "--name", sessionName)
	mustOK(t, bin, env, "stop", "--name", sessionName)

	// Bundle must exist and be a readable zip.
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("trace bundle missing at %s: %v", tracePath, err)
	}
	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()

	man := readManifest(t, &zr.Reader)
	if man.Version != 1 {
		t.Errorf("manifest version = %d, want 1", man.Version)
	}
	if !containsCmd(man.Command, "vim") {
		t.Errorf("manifest command does not include vim: %v", man.Command)
	}
	if man.Cols != 80 || man.Rows != 24 {
		t.Errorf("manifest size = %dx%d, want 80x24", man.Cols, man.Rows)
	}
	if man.Pid <= 0 {
		t.Errorf("manifest pid = %d, want > 0", man.Pid)
	}
	if man.StartedAt.IsZero() || man.StoppedAt.IsZero() {
		t.Errorf("manifest timestamps zero: started=%v stopped=%v", man.StartedAt, man.StoppedAt)
	}
	if man.StoppedAt.Before(man.StartedAt) {
		t.Errorf("stopped_at (%v) before started_at (%v)", man.StoppedAt, man.StartedAt)
	}
	if len(man.Screenshots) < 2 {
		t.Errorf("screenshots = %v, want at least start + stop frames", man.Screenshots)
	}
	for _, p := range man.Screenshots {
		sf, err := zr.Open(p)
		if err != nil {
			t.Errorf("screenshot %q missing: %v", p, err)
			continue
		}
		if _, err := png.Decode(sf); err != nil {
			t.Errorf("screenshot %q not a valid PNG: %v", p, err)
		}
		sf.Close()
	}

	nOut, nIn, typedSeen := scanEvents(t, &zr.Reader)
	if nOut == 0 {
		t.Error("trace recorded no output events")
	}
	if nIn == 0 {
		t.Error("trace recorded no input events")
	}
	if !strings.Contains(typedSeen, typed) {
		t.Errorf("typed input %q not in trace; saw %q", typed, typedSeen)
	}
}

// TestTraceFinalizedWhenChildExits records a session whose child exits on
// its own, without an explicit `trace stop`. The bundle must be durable —
// complete with the final screenshot — by the moment `wait exit` returns.
func TestTraceFinalizedWhenChildExits(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	tracePath := filepath.Join(t.TempDir(), "session.twee")
	const sessionName = "trace-child-exit"
	defer exec.Command(bin, "stop", "--name", sessionName).Run()

	mustOK(t, bin, env, "start",
		"--name", sessionName,
		"--",
		"/bin/sh", "-c", "echo tracing; sleep 2",
	)
	mustOK(t, bin, env, "trace", "start",
		"--name", sessionName,
		"--out", tracePath,
	)

	resp, out, err := runCLI(t, bin, env, "wait", "exit", "--name", sessionName)
	if err != nil {
		t.Fatalf("wait exit: %v\n%s", err, out)
	}
	if resp["ok"] != true {
		t.Fatalf("wait exit not ok: %s", out)
	}
	data, _ := resp["data"].(map[string]any)
	if got, _ := data["trace_path"].(string); got != tracePath {
		t.Errorf("wait exit trace_path = %q, want %q", got, tracePath)
	}

	// No sleeping, no retries: the contract is that the bundle already
	// exists when wait exit returns.
	if _, err := os.Stat(tracePath); err != nil {
		t.Fatalf("trace bundle missing when wait exit returned: %v", err)
	}
	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()

	man := readManifest(t, &zr.Reader)
	if man.StoppedAt.IsZero() {
		t.Error("manifest stopped_at is zero")
	}
	if len(man.Screenshots) < 2 {
		t.Errorf("screenshots = %v, want initial + final frames", man.Screenshots)
	}
	for _, p := range man.Screenshots {
		sf, err := zr.Open(p)
		if err != nil {
			t.Errorf("screenshot %q missing: %v", p, err)
			continue
		}
		if _, err := png.Decode(sf); err != nil {
			t.Errorf("screenshot %q not a valid PNG: %v", p, err)
		}
		sf.Close()
	}
	if nOut, _, _ := scanEvents(t, &zr.Reader); nOut == 0 {
		t.Error("trace recorded no output events")
	}
}

// TestStartTraceFullSession records a session from spawn to teardown via
// `start --trace`, with no trace verbs at all.
func TestStartTraceFullSession(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	tracePath := filepath.Join(t.TempDir(), "run.twee")
	const sessionName = "start-trace"
	defer exec.Command(bin, "stop", "--name", sessionName).Run()

	resp, out, err := runCLI(t, bin, env, "start",
		"--name", sessionName,
		"--trace", tracePath,
		"--",
		"/bin/sh", "-c", "echo start-trace-output; sleep 2",
	)
	if err != nil {
		t.Fatalf("start --trace: %v\n%s", err, out)
	}
	data, _ := resp["data"].(map[string]any)
	if got, _ := data["trace"].(string); got != tracePath {
		t.Errorf("start response trace = %q, want %q", got, tracePath)
	}

	resp, out, err = runCLI(t, bin, env, "wait", "exit", "--name", sessionName)
	if err != nil {
		t.Fatalf("wait exit: %v\n%s", err, out)
	}
	data, _ = resp["data"].(map[string]any)
	if got, _ := data["trace_path"].(string); got != tracePath {
		t.Errorf("wait exit trace_path = %q, want %q", got, tracePath)
	}

	zr, err := zip.OpenReader(tracePath)
	if err != nil {
		t.Fatalf("open trace zip: %v", err)
	}
	defer zr.Close()
	man := readManifest(t, &zr.Reader)
	if man.StoppedAt.IsZero() {
		t.Error("manifest stopped_at is zero")
	}
	if len(man.Screenshots) < 2 {
		t.Errorf("screenshots = %v, want initial + final frames", man.Screenshots)
	}
	nOut, _, _ := scanEvents(t, &zr.Reader)
	if nOut == 0 {
		t.Error("trace recorded no output events")
	}
}

// TestStartTraceQuickExit: even when the child dies inside start's
// observation window, the requested trace bundle is written and the
// CHILD_EXITED error points at it.
func TestStartTraceQuickExit(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	tracePath := filepath.Join(t.TempDir(), "quick.twee")
	const sessionName = "start-trace-qe"
	defer exec.Command(bin, "stop", "--name", sessionName).Run()

	stdout := cliStdout(t, bin, env, "start",
		"--name", sessionName,
		"--trace", tracePath,
		"--",
		"/bin/sh", "-c", "exit 3",
	)
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ExitCode  *int   `json:"exit_code"`
				TracePath string `json:"trace_path"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		t.Fatalf("decode start envelope %s: %v", stdout, err)
	}
	if resp.OK || resp.Error.Code != "CHILD_EXITED" {
		t.Fatalf("start envelope = %s, want CHILD_EXITED error", stdout)
	}
	if resp.Error.Details.ExitCode == nil || *resp.Error.Details.ExitCode != 3 {
		t.Errorf("details exit_code = %v, want 3", resp.Error.Details.ExitCode)
	}
	if resp.Error.Details.TracePath != tracePath {
		t.Errorf("details trace_path = %q, want %q", resp.Error.Details.TracePath, tracePath)
	}
	if _, err := zip.OpenReader(tracePath); err != nil {
		t.Errorf("quick-exit trace bundle not written: %v", err)
	}
}

// TestTraceStopAfterDaemonGone pins the failure mode of `trace stop` once
// the session has torn down: a NOT_FOUND error that tells the user the
// trace was already finalized, instead of a bare dial error.
func TestTraceStopAfterDaemonGone(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)

	_, out, err := runCLI(t, bin, env, "trace", "stop", "--name", "no-such-session")
	if err == nil {
		t.Fatalf("trace stop on missing session succeeded: %s", out)
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	stdout := cliStdout(t, bin, env, "trace", "stop", "--name", "no-such-session")
	if err := json.Unmarshal(stdout, &resp); err != nil {
		t.Fatalf("decode error envelope %s: %v", stdout, err)
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("error code = %q, want NOT_FOUND", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "finalized automatically") {
		t.Errorf("error message lacks finalization hint: %q", resp.Error.Message)
	}
}

// cliStdout runs the CLI expecting a non-zero exit and returns its stdout,
// where twee prints its JSON error envelope.
func cliStdout(t *testing.T, bin string, env []string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, _ := cmd.Output()
	return out
}

type traceManifest struct {
	Version     int       `json:"version"`
	Command     []string  `json:"command"`
	Cols        int       `json:"cols"`
	Rows        int       `json:"rows"`
	Pid         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
	StoppedAt   time.Time `json:"stopped_at"`
	Screenshots []string  `json:"screenshots"`
}

func readManifest(t *testing.T, zr *zip.Reader) traceManifest {
	t.Helper()
	mf, err := zr.Open("manifest.json")
	if err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	defer mf.Close()
	var m traceManifest
	if err := json.NewDecoder(mf).Decode(&m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return m
}

// scanEvents tallies output / input events and reassembles the bytes
// recorded for `type` input events so the caller can assert on what the
// session typed.
func scanEvents(t *testing.T, zr *zip.Reader) (nOut, nIn int, typed string) {
	t.Helper()
	ef, err := zr.Open("events.jsonl")
	if err != nil {
		t.Fatalf("events.jsonl missing: %v", err)
	}
	defer ef.Close()
	sc := bufio.NewScanner(ef)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var typedB strings.Builder
	for sc.Scan() {
		var ev struct {
			Type  string `json:"type"`
			Kind  string `json:"kind"`
			Bytes string `json:"bytes_b64"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode event: %v\n%s", err, sc.Bytes())
		}
		switch ev.Type {
		case "output":
			nOut++
		case "input":
			nIn++
			if ev.Kind == "type" && ev.Bytes != "" {
				b, err := base64.StdEncoding.DecodeString(ev.Bytes)
				if err != nil {
					t.Fatalf("decode input bytes: %v", err)
				}
				typedB.Write(b)
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan events: %v", err)
	}
	return nOut, nIn, typedB.String()
}

func containsCmd(cmd []string, want string) bool {
	for _, a := range cmd {
		if strings.Contains(a, want) {
			return true
		}
	}
	return false
}
