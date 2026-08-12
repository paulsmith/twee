package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCommandRegistryIsComplete(t *testing.T) {
	for key, d := range commandRegistry {
		if got := descriptorKey(d.Path); got != key {
			t.Errorf("descriptor key %q has path %q", key, got)
		}
		if d.Usage == "" {
			t.Errorf("descriptor %q has no usage", key)
		}
		if len(d.Path) == 1 && d.handler == nil {
			t.Errorf("top-level descriptor %q has no dispatch handler", key)
		}
		if d.SuccessOutput == "" || d.Formats == nil || len(d.ExitStatus) == 0 {
			t.Errorf("descriptor %q has incomplete output metadata: %+v", key, d)
		}
	}
}

func TestMachineExportSuccessReportsArtifact(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "input.twee")
	writeContractBundle(t, bundle)

	outPath := filepath.Join(dir, "replay.html")
	cmd := exec.Command(buildBinary(t), "--machine", "export", bundle, "-o", outPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("export: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Path          string `json:"path"`
			Format        string `json:"format"`
			OmittedEvents *int   `json:"omitted_events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if !envelope.OK || envelope.Data.Path != outPath || envelope.Data.Format != "html" || envelope.Data.OmittedEvents == nil || *envelope.Data.OmittedEvents != 0 {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestMachineExportCastReportsOmittedEvents(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "input.twee")
	writeContractBundle(t, bundle)

	outPath := filepath.Join(dir, "recording.cast")
	cmd := exec.Command(buildBinary(t), "--machine", "export", bundle, "-o", outPath)
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Format        string `json:"format"`
			OmittedEvents int    `json:"omitted_events"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if !envelope.OK || envelope.Data.Format != "cast" || envelope.Data.OmittedEvents != 1 {
		t.Fatalf("envelope = %+v", envelope)
	}
}
func writeContractBundle(t *testing.T, bundle string) {
	f, err := os.Create(bundle)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	manifest, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(manifest, `{"version":1,"command":["true"],"cols":10,"rows":2}`); err != nil {
		t.Fatal(err)
	}
	events, err := zw.Create("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(events, `{"t_ms":0,"type":"output","bytes_b64":"%s"}`+"\n", base64.StdEncoding.EncodeToString([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(events, `{"t_ms":1,"type":"exit","code":0}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultExportOutputContracts(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "input.twee")
	writeContractBundle(t, bundle)
	success := runContractCLI(t, buildBinary(t), nil, "export", bundle, "-o", filepath.Join(dir, "replay.html"))
	if success.exitCode != 0 || len(success.stdout) != 0 || len(success.stderr) != 0 {
		t.Fatalf("success contract: exit=%d stdout=%q stderr=%q", success.exitCode, success.stdout, success.stderr)
	}
	failure := runContractCLI(t, buildBinary(t), nil, "export", filepath.Join(dir, "missing.twee"), "-o", filepath.Join(dir, "failed.html"))
	if failure.exitCode != 1 || len(failure.stdout) != 0 || len(failure.stderr) == 0 {
		t.Fatalf("failure contract: exit=%d stdout=%q stderr=%q", failure.exitCode, failure.stdout, failure.stderr)
	}
	if json.Valid(failure.stderr) {
		t.Fatalf("default runtime failure unexpectedly emitted JSON: %s", failure.stderr)
	}
}

func TestJSONHelpSchemaAndPath(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "--format", "json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var all helpDocument
	if err := json.Unmarshal(out, &all); err != nil {
		t.Fatalf("decode all help: %v\n%s", err, out)
	}
	if all.SchemaVersion != 1 || len(all.Commands) != len(commandRegistry) {
		t.Fatalf("help document = version %d, %d commands; want 1, %d", all.SchemaVersion, len(all.Commands), len(commandRegistry))
	}
	if screenshot := commandRegistry["screenshot"]; screenshot.Artifact == nil || screenshot.Artifact.PathField != "data.out" {
		t.Fatalf("screenshot artifact descriptor = %+v, want data.out", screenshot.Artifact)
	}

	out, err = exec.Command(bin, "help", "wait", "text", "--format=json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var one commandHelpDocument
	if err := json.Unmarshal(out, &one); err != nil {
		t.Fatalf("decode command help: %v\n%s", err, out)
	}
	if one.SchemaVersion != 1 || !reflect.DeepEqual(one.Command.Path, []string{"wait", "text"}) {
		t.Fatalf("command help = %+v", one)
	}
}

func TestMachineOutputContracts(t *testing.T) {
	bin := buildBinary(t)
	tests := []struct {
		name     string
		args     []string
		exitCode int
		ok       bool
	}{
		{name: "text success", args: []string{"--machine", "version"}, ok: true},
		{name: "explicit JSON help success", args: []string{"--machine", "help", "--format", "json"}, ok: true},
		{name: "root usage", args: []string{"--machine", "--bad"}, exitCode: 2},
		{name: "malformed name still detects machine", args: []string{"--name", "--machine", "status"}, exitCode: 2},
		{name: "command usage", args: []string{"--machine", "sleep", "bad"}, exitCode: 2},
		{name: "runtime", args: []string{"--machine", "export", "missing.twee", "-o", "out.html"}, exitCode: 1},
		{name: "interactive rejection", args: []string{"--machine", "play", "missing.twee"}, exitCode: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bin, tt.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			gotExit := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				gotExit = exit.ExitCode()
			}
			if gotExit != tt.exitCode {
				t.Fatalf("exit = %d, want %d; stdout=%s stderr=%s", gotExit, tt.exitCode, stdout.String(), stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			var envelope struct {
				OK bool `json:"ok"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("stdout is not one JSON value: %v\n%s", err, stdout.String())
			}
			if envelope.OK != tt.ok {
				t.Fatalf("ok = %v, want %v: %s", envelope.OK, tt.ok, stdout.String())
			}
		})
	}
}
