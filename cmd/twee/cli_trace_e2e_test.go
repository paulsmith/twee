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
		"vim", "-u", "NONE", "-N", "-n", bufFile,
	)

	// Vim's empty-buffer screen has tilde markers down the left edge.
	mustOK(t, bin, env, "wait", "text", "--name", sessionName, "~")

	mustOK(t, bin, env, "trace", "start",
		"--name", sessionName,
		"--out", tracePath,
	)

	mustOK(t, bin, env, "type", "--name", sessionName, "i")
	mustOK(t, bin, env, "type", "--name", sessionName, typed)
	mustOK(t, bin, env, "key", "--name", sessionName, "Escape")
	mustOK(t, bin, env, "wait", "text", "--name", sessionName, typed)

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
