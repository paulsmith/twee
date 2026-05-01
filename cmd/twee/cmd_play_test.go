package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/paulsmith/research/twee/internal/play"
)

func TestPlayFakeKittyEmitsAPC(t *testing.T) {
	bin := buildBinary(t)
	path := writePlayBundle(t)

	cmd := exec.Command(bin, "play", "--speed", "100", path)
	cmd.Env = append(os.Environ(), append(testEnv(t), "TWEE_PLAY_FAKE_KITTY=1")...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("play: %v\nstderr:\n%s\nstdout:\n%s", err, exitErr.Stderr, out)
		}
		t.Fatalf("play: %v", err)
	}
	if !bytes.Contains(out, []byte("\x1b_Ga=T")) {
		t.Fatalf("play output did not contain Kitty APC image data:\n%q", out)
	}
	if !bytes.Contains(out, []byte("at end")) {
		t.Fatalf("play output did not contain final status:\n%q", out)
	}
}

func TestParsePlayArgsAcceptsFlagsAfterBundle(t *testing.T) {
	path, opts := parsePlayArgs([]string{"demo.twee", "--speed", "2.5", "--step", "--max-idle=500ms", "-v"})
	if path != "demo.twee" {
		t.Fatalf("path = %q, want demo.twee", path)
	}
	if opts.Speed != 2.5 {
		t.Fatalf("speed = %v, want 2.5", opts.Speed)
	}
	if !opts.Step {
		t.Fatal("step = false, want true")
	}
	if opts.MaxIdle != 500000000 {
		t.Fatalf("maxIdle = %s, want 500ms", opts.MaxIdle)
	}
	if !opts.Verbose {
		t.Fatal("verbose = false, want true")
	}
}

func TestValidSpeedRejectsNonFiniteAndNonPositive(t *testing.T) {
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if play.ValidSpeed(v) {
			t.Fatalf("speed %v unexpectedly valid", v)
		}
	}
	for _, v := range []float64{1, 0.5} {
		if !play.ValidSpeed(v) {
			t.Fatalf("speed %v unexpectedly invalid", v)
		}
	}
}

func writePlayBundle(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mw, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mw.Write([]byte(`{"version":1,"command":["printf"],"cols":10,"rows":3}`)); err != nil {
		t.Fatal(err)
	}
	ew, err := zw.Create("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	event := `{"t_ms":0,"type":"output","bytes_b64":"` + base64.StdEncoding.EncodeToString([]byte("hi")) + `"}`
	if _, err := ew.Write([]byte(event + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
