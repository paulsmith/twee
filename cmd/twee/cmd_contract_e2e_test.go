package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

type contractEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestCursorCLIResponseMatchesDocumentedShape(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "cursor-contract"
	defer stopContractSession(bin, env, name)

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "printf '\033[5 qready'; sleep 30")
	mustOK(t, bin, env, "wait", "text", "--name", name, "--pattern", "ready")

	result := runContractCLI(t, bin, env, "cursor", "--name", name)
	if result.exitCode != 0 {
		t.Fatalf("cursor exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
	}
	envelope := decodeContractEnvelope(t, result.stdout)
	if !envelope.OK {
		t.Fatalf("cursor response = %s, want ok response", result.stdout)
	}
	assertJSONKeys(t, envelope.Data, "shape", "visible", "x", "y")

	var cursor rpc.CursorData
	if err := json.Unmarshal(envelope.Data, &cursor); err != nil {
		t.Fatalf("decode cursor data: %v\n%s", err, envelope.Data)
	}
	if cursor.Shape != "bar" {
		t.Fatalf("cursor shape = %q, want bar", cursor.Shape)
	}
}

func TestResizeCLIResponseAcknowledgesCommittedDimensions(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "resize-contract"
	defer stopContractSession(bin, env, name)

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "sleep 30")
	result := runContractCLI(t, bin, env, "resize", "--name", name, "--cols", "100", "--rows", "30")
	if result.exitCode != 0 {
		t.Fatalf("resize exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
	}
	envelope := decodeContractEnvelope(t, result.stdout)
	if !envelope.OK {
		t.Fatalf("resize response = %s, want ok response", result.stdout)
	}
	assertJSONKeys(t, envelope.Data, "cols", "rows")
	var ack rpc.SizeData
	if err := json.Unmarshal(envelope.Data, &ack); err != nil {
		t.Fatalf("decode resize acknowledgement: %v\n%s", err, envelope.Data)
	}
	if ack.Cols != 100 || ack.Rows != 30 {
		t.Fatalf("resize acknowledgement = %+v, want 100x30", ack)
	}
}

func TestResizeHelpDefinesAcknowledgementBoundary(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "resize").Output()
	if err != nil {
		t.Fatalf("help resize: %v", err)
	}
	help := string(out)
	for _, phrase := range []string{
		"returns the acknowledged {cols, rows}",
		"does not wait for the child to",
		"use a wait command for observable UI state",
	} {
		if !strings.Contains(help, phrase) {
			t.Errorf("resize help missing %q:\n%s", phrase, help)
		}
	}
}

func TestDiffCLIResponseAndExitContract(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	name := "diff-contract"
	defer stopContractSession(bin, env, name)

	mustOK(t, bin, env, "start", "--name", name, "--", "/bin/sh", "-c", "printf 'current'; sleep 30")
	mustOK(t, bin, env, "wait", "text", "--name", name, "--pattern", "current")

	textResult := runContractCLI(t, bin, env, "text", "--name", name)
	if textResult.exitCode != 0 {
		t.Fatalf("text exit code = %d, want 0\nstdout: %s\nstderr: %s", textResult.exitCode, textResult.stdout, textResult.stderr)
	}
	textEnvelope := decodeContractEnvelope(t, textResult.stdout)
	var textData rpc.TextData
	if err := json.Unmarshal(textEnvelope.Data, &textData); err != nil {
		t.Fatalf("decode text data: %v\n%s", err, textEnvelope.Data)
	}

	equalPath := filepath.Join(t.TempDir(), "equal.txt")
	if err := os.WriteFile(equalPath, []byte(textData.Text), 0o600); err != nil {
		t.Fatal(err)
	}
	equal := runContractCLI(t, bin, env, "diff", "--name", name, "--against", equalPath)
	assertDiffResult(t, equal, true)

	unequalPath := filepath.Join(t.TempDir(), "unequal.txt")
	if err := os.WriteFile(unequalPath, []byte("expected"), 0o600); err != nil {
		t.Fatal(err)
	}
	unequal := runContractCLI(t, bin, env, "diff", "--name", name, "--against", unequalPath)
	assertDiffResult(t, unequal, false)

	missing := runContractCLI(t, bin, env, "diff", "--name", name, "--against", filepath.Join(t.TempDir(), "missing.txt"))
	if missing.exitCode == 0 {
		t.Fatalf("missing comparison file exit code = 0, want nonzero\nstdout: %s", missing.stdout)
	}
	missingEnvelope := decodeContractEnvelope(t, missing.stdout)
	if missingEnvelope.OK || missingEnvelope.Error == nil || missingEnvelope.Error.Code != "IO" {
		t.Fatalf("missing comparison file response = %s, want IO error", missing.stdout)
	}

	usage := runContractCLI(t, bin, env, "diff", "--name", name)
	if usage.exitCode != 2 {
		t.Fatalf("diff usage exit code = %d, want 2\nstdout: %s\nstderr: %s", usage.exitCode, usage.stdout, usage.stderr)
	}
	if len(usage.stdout) != 0 || !strings.Contains(string(usage.stderr), "AGAINST is required") {
		t.Fatalf("diff usage output mismatch\nstdout: %s\nstderr: %s", usage.stdout, usage.stderr)
	}
}

func TestDiffHelpDescribesComparisonAndFailureExitStatuses(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "diff").Output()
	if err != nil {
		t.Fatalf("help diff: %v", err)
	}
	help := string(out)
	for _, phrase := range []string{
		"completed comparison exits 0 whether equal or unequal",
		"Usage, file, session, and transport failures exit nonzero",
		"{equal: bool, unified: string, current: string, expected: string}",
	} {
		if !strings.Contains(help, phrase) {
			t.Errorf("diff help missing %q:\n%s", phrase, help)
		}
	}
	if strings.Contains(help, "Always exits 0") {
		t.Errorf("diff help retains misleading exit claim:\n%s", help)
	}
}

type contractCLIResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

func runContractCLI(t *testing.T, bin string, env []string, args ...string) contractCLIResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return contractCLIResult{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%v: %v", args, err)
	}
	return contractCLIResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitErr.ExitCode()}
}

func decodeContractEnvelope(t *testing.T, raw []byte) contractEnvelope {
	t.Helper()
	var envelope contractEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode response: %v\n%s", err, raw)
	}
	return envelope
}

func assertJSONKeys(t *testing.T, raw json.RawMessage, want ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode response fields: %v\n%s", err, raw)
	}
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("response fields = %v, want %v", got, want)
	}
}

func assertDiffResult(t *testing.T, result contractCLIResult, wantEqual bool) {
	t.Helper()
	if result.exitCode != 0 {
		t.Fatalf("diff exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
	}
	envelope := decodeContractEnvelope(t, result.stdout)
	if !envelope.OK {
		t.Fatalf("diff response = %s, want ok response", result.stdout)
	}
	assertJSONKeys(t, envelope.Data, "current", "equal", "expected", "unified")
	var diff rpc.DiffData
	if err := json.Unmarshal(envelope.Data, &diff); err != nil {
		t.Fatalf("decode diff data: %v\n%s", err, envelope.Data)
	}
	if diff.Equal != wantEqual {
		t.Fatalf("diff equal = %v, want %v\n%s", diff.Equal, wantEqual, envelope.Data)
	}
	if !wantEqual && diff.Unified == "" {
		t.Fatalf("unequal diff has empty unified output: %s", envelope.Data)
	}
}

func stopContractSession(bin string, env []string, name string) {
	cmd := exec.Command(bin, "stop", "--name", name)
	cmd.Env = append(os.Environ(), env...)
	_ = cmd.Run()
}
