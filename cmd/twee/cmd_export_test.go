package main

import (
	"testing"
	"time"
)

func TestParseExportArgs(t *testing.T) {
	path, out, opts := parseExportArgs([]string{
		"demo.twee", "-o", "demo.mp4",
		"--speed", "2", "--max-idle", "1s", "--font-size", "12", "--fps-cap", "15",
	})
	if path != "demo.twee" || out != "demo.mp4" {
		t.Errorf("path/out = %q/%q", path, out)
	}
	if opts.Speed != 2 || opts.MaxIdle != time.Second ||
		opts.FontSize != 12 || opts.FPSCap != 15 {
		t.Errorf("opts = %+v", opts)
	}
}

func TestParseExportArgsDefaults(t *testing.T) {
	_, _, opts := parseExportArgs([]string{"demo.twee", "-o", "demo.gif"})
	if opts.Speed != 1 || opts.MaxIdle != 0 || opts.FPSCap != 30 {
		t.Errorf("defaults wrong: %+v", opts)
	}
}

func TestParseRootAllowsExportShortOutputFlag(t *testing.T) {
	root, err := parseRootArgs([]string{"export", "demo.twee", "-o", "demo.gif"})
	if err != nil {
		t.Fatal(err)
	}
	if root.Verb != "export" {
		t.Fatalf("verb = %q, want export", root.Verb)
	}
}
