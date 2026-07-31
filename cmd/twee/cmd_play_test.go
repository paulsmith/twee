package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/play"
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

func TestPlayFakeExperimentalBackendsEmitProtocolBytes(t *testing.T) {
	bin := buildBinary(t)
	path := writePlayBundle(t)
	for _, tt := range []struct {
		backend string
		want    []byte
	}{
		{"iterm2", []byte("\x1b]1337;File=")},
		{"sixel", []byte("\x1bP0;1;0q")},
	} {
		t.Run(tt.backend, func(t *testing.T) {
			cmd := exec.Command(bin, "play", path, "--speed", "100", "--backend", tt.backend)
			cmd.Env = append(os.Environ(), append(testEnv(t), "TWEE_PLAY_FAKE_BACKEND="+tt.backend)...)
			out, err := cmd.Output()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					t.Fatalf("play: %v\nstderr:\n%s\nstdout:\n%s", err, exitErr.Stderr, out)
				}
				t.Fatalf("play: %v", err)
			}
			if !bytes.Contains(out, tt.want) {
				t.Fatalf("output missing %q in %q", tt.want, out)
			}
			if !bytes.Contains(out, []byte("at end")) {
				t.Fatalf("output missing final status in %q", out)
			}
		})
	}
}

func TestParsePlayArgsAcceptsFlagsAfterBundle(t *testing.T) {
	path, opts := parsePlayArgs([]string{"demo.twee", "--backend", "sixel", "--speed", "2.5", "--step", "--max-idle=500ms", "--no-mouse-annotations", "--verbose"})
	if path != "demo.twee" {
		t.Fatalf("path = %q, want demo.twee", path)
	}
	if opts.Speed != 2.5 {
		t.Fatalf("speed = %v, want 2.5", opts.Speed)
	}
	if opts.Backend != play.BackendSixel {
		t.Fatalf("backend = %q, want sixel", opts.Backend)
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
	if !opts.DisableMouseAnnotations {
		t.Fatal("DisableMouseAnnotations = false, want true")
	}
}

func TestParsePlayArgsEnablesMouseAnnotationsByDefault(t *testing.T) {
	_, opts := parsePlayArgs([]string{"demo.twee"})
	if opts.DisableMouseAnnotations {
		t.Fatal("DisableMouseAnnotations = true, want false by default")
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

func TestPlayHelpDocumentsBackendConstraints(t *testing.T) {
	help := usages["play"]
	for _, want := range []string{
		"auto tries Kitty, then iTerm2, then Sixel",
		"direct terminal", "tmux and screen", "native pixel geometry", "--no-mouse-annotations",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("play help missing %q:\n%s", want, help)
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
