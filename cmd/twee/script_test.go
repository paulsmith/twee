package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestDecodeScriptRejectsUnknownRequestField(t *testing.T) {
	_, err := decodeScript([]byte(`[{
		"op":"status",
		"argument":{}
	}]`))
	if err == nil || !strings.Contains(err.Error(), "argument") {
		t.Fatalf("decodeScript error = %v, want unknown argument field", err)
	}
}

func TestDecodeScriptRejectsTrailingDocument(t *testing.T) {
	_, err := decodeScript([]byte(`[] []`))
	if err == nil {
		t.Fatal("decodeScript accepted a trailing JSON document")
	}
}

func TestNormalizeScriptPaths(t *testing.T) {
	clientDir := filepath.Join(string(filepath.Separator), "client", "cwd")
	absolute := filepath.Join(string(filepath.Separator), "already", "absolute.png")
	ops, err := decodeScript([]byte(`[
		{"op":"screenshot","args":{"out":"shot.png","pixel_width":320,"pixel_height":200}},
		{"op":"trace_start","args":{"out":"traces/session.twee"}},
		{"op":"diff","args":{"against":"expected.txt"}},
		{"op":"screenshot","args":{"out":"` + filepath.ToSlash(absolute) + `"}},
		{"op":"status","args":{"preserve":"raw"}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	statusArgs := append(json.RawMessage(nil), ops[4].Args...)

	if err := normalizeScriptPaths(ops, clientDir); err != nil {
		t.Fatal(err)
	}

	var screenshot rpc.ScreenshotArgs
	if err := json.Unmarshal(ops[0].Args, &screenshot); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(clientDir, "shot.png"); screenshot.Out != want {
		t.Errorf("screenshot out = %q, want %q", screenshot.Out, want)
	}
	if screenshot.PixelWidth != 320 || screenshot.PixelHeight != 200 {
		t.Errorf("screenshot dimensions = %dx%d, want 320x200", screenshot.PixelWidth, screenshot.PixelHeight)
	}

	var trace rpc.TraceStartArgs
	if err := json.Unmarshal(ops[1].Args, &trace); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(clientDir, "traces", "session.twee"); trace.Out != want {
		t.Errorf("trace out = %q, want %q", trace.Out, want)
	}

	var diff rpc.DiffArgs
	if err := json.Unmarshal(ops[2].Args, &diff); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(clientDir, "expected.txt"); diff.Against != want {
		t.Errorf("diff against = %q, want %q", diff.Against, want)
	}

	if err := json.Unmarshal(ops[3].Args, &screenshot); err != nil {
		t.Fatal(err)
	}
	if screenshot.Out != absolute {
		t.Errorf("absolute screenshot out = %q, want unchanged %q", screenshot.Out, absolute)
	}
	if string(ops[4].Args) != string(statusArgs) {
		t.Errorf("unrelated args changed from %s to %s", statusArgs, ops[4].Args)
	}
}

func TestNormalizeScriptPathsAllowsOmittedOptionalArgs(t *testing.T) {
	ops := []rpc.Request{{Op: rpc.OpScreenshot}, {Op: rpc.OpTraceStart}}
	if err := normalizeScriptPaths(ops, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeScriptPathsRejectsUnknownPathArg(t *testing.T) {
	for _, op := range []string{rpc.OpScreenshot, rpc.OpTraceStart, rpc.OpDiff} {
		t.Run(op, func(t *testing.T) {
			ops := []rpc.Request{{Op: op, Args: json.RawMessage(`{"unknown":"path"}`)}}
			err := normalizeScriptPaths(ops, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("normalizeScriptPaths error = %v, want unknown-field error", err)
			}
		})
	}
}
