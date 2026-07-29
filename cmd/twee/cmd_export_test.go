package main

import (
	"strings"
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/export"
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

func TestParseExportArgsCrop(t *testing.T) {
	_, _, opts := parseExportArgs([]string{
		"demo.twee", "-o", "demo.gif", "--crop", "1,2,10,5",
	})
	if opts.Crop == nil {
		t.Fatal("Crop = nil, want set")
	}
	want := export.CropRect{X: 1, Y: 2, W: 10, H: 5}
	if *opts.Crop != want {
		t.Errorf("Crop = %+v, want %+v", *opts.Crop, want)
	}
}

func TestParseExportArgsInputOverlay(t *testing.T) {
	_, _, opts := parseExportArgs([]string{"demo.twee", "-o", "demo.gif", "--input-overlay"})
	if !opts.InputOverlay {
		t.Error("InputOverlay = false, want true")
	}
	_, _, opts = parseExportArgs([]string{"demo.twee", "-o", "demo.gif"})
	if opts.InputOverlay {
		t.Error("InputOverlay = true by default, want false")
	}
}

func TestParseCropFlagValid(t *testing.T) {
	rect, err := parseCropFlag("1,2,3,4")
	if err != nil {
		t.Fatal(err)
	}
	want := export.CropRect{X: 1, Y: 2, W: 3, H: 4}
	if rect != want {
		t.Errorf("rect = %+v, want %+v", rect, want)
	}
}

func TestParseExportArgsQuality(t *testing.T) {
	_, _, opts := parseExportArgs([]string{"demo.twee", "-o", "demo.mp4", "--quality", "high"})
	if opts.Quality != "high" {
		t.Errorf("Quality = %q, want high", opts.Quality)
	}
	// Not passing --quality at all must leave Options.Quality empty here;
	// export.Options.normalize (inside the export package) is what
	// applies the "medium" default, not the CLI layer.
	_, _, opts = parseExportArgs([]string{"demo.twee", "-o", "demo.mp4"})
	if opts.Quality != "" {
		t.Errorf("Quality = %q, want empty when --quality omitted", opts.Quality)
	}
}

func TestParseQualityFlagValid(t *testing.T) {
	for _, q := range []string{"low", "medium", "high"} {
		got, err := parseQualityFlag(q, "out.mp4")
		if err != nil {
			t.Fatalf("parseQualityFlag(%q, out.mp4): %v", q, err)
		}
		if got != q {
			t.Errorf("got %q, want %q", got, q)
		}
		if _, err := parseQualityFlag(q, "out.webm"); err != nil {
			t.Errorf("parseQualityFlag(%q, out.webm): %v", q, err)
		}
	}
}

func TestParseQualityFlagRejectsGIF(t *testing.T) {
	_, err := parseQualityFlag("high", "out.gif")
	if err == nil {
		t.Fatal("want error for --quality with .gif output")
	}
	if !strings.Contains(err.Error(), "gif") {
		t.Errorf("error = %v, want it to name .gif as the reason", err)
	}
}

func TestParseQualityFlagRejectsUnknownValue(t *testing.T) {
	if _, err := parseQualityFlag("ultra", "out.mp4"); err == nil {
		t.Fatal("want error for an unrecognized --quality value")
	}
}

func TestParseCropFlagInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"too few fields", "1,2,3"},
		{"too many fields", "1,2,3,4,5"},
		{"non-integer", "a,b,c,d"},
		{"zero width", "0,0,0,5"},
		{"zero height", "0,0,5,0"},
		{"negative width", "0,0,-1,5"},
		{"negative x", "-1,0,5,5"},
		{"negative y", "0,-1,5,5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseCropFlag(tt.in); err == nil {
				t.Fatalf("parseCropFlag(%q) succeeded, want error", tt.in)
			}
		})
	}
}
