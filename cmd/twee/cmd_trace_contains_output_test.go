package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func TestFindTraceOutputSpansAllBundleEvents(t *testing.T) {
	events := []tracebundle.Event{
		{TMS: 10, Type: trace.EventTypeOutput, Bytes: []byte("\x1b[")},
		{TMS: 20, Type: trace.EventTypeInput, Bytes: []byte("ignored")},
		{TMS: 30, Type: trace.EventTypeResize, Cols: 100, Rows: 40},
		{TMS: 40, Type: trace.EventTypeOutput},
		{TMS: 50, Type: trace.EventTypeOutput, Bytes: []byte("?200")},
		{TMS: 55, Type: trace.EventTypeExit},
		{TMS: 60, Type: trace.EventTypeOutput, Bytes: []byte("4h")},
	}

	got, ok := findTraceOutput(events, []byte("\x1b[?2004h"), nil)
	if !ok {
		t.Fatal("findTraceOutput did not match")
	}
	want := (traceOutputMatch{TMS: 60, EventStart: 0, EventEnd: 6})
	if got != want {
		t.Fatalf("match = %+v, want %+v", got, want)
	}
}

func TestFindTraceOutputUsesFirstLeftmostRegexMatch(t *testing.T) {
	events := []tracebundle.Event{
		{TMS: 10, Type: trace.EventTypeOutput, Bytes: []byte("xxa")},
		{TMS: 20, Type: trace.EventTypeOutput, Bytes: []byte("aayy")},
	}
	got, ok := findTraceOutput(events, nil, regexp.MustCompile(`a+`))
	if !ok {
		t.Fatal("findTraceOutput did not match")
	}
	if want := (traceOutputMatch{TMS: 20, EventStart: 0, EventEnd: 1}); got != want {
		t.Fatalf("match = %+v, want %+v", got, want)
	}
}

func TestRegexpCanMatchEmpty(t *testing.T) {
	for _, test := range []struct {
		expr string
		want bool
	}{
		{"^", true}, {`\b`, true}, {"a*", true}, {"(?:a|)", true},
		{"a+", false}, {"^error", false}, {`\bword`, false}, {"(?:ab|cd)", false},
	} {
		t.Run(test.expr, func(t *testing.T) {
			parsed, err := syntaxParse(test.expr)
			if err != nil {
				t.Fatal(err)
			}
			if got := regexpCanMatchEmpty(parsed); got != test.want {
				t.Fatalf("regexpCanMatchEmpty(%q) = %t, want %t", test.expr, got, test.want)
			}
		})
	}
}

func TestTraceContainsOutputMatchers(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeTraceOutputBundle(t)
	for _, test := range []struct {
		name string
		args []string
		want traceOutputMatch
	}{
		{"text", []string{"--text", "é ready"}, traceOutputMatch{TMS: 40, EventStart: 0, EventEnd: 3}},
		{"hex", []string{"--hex", "1b5b3f3230303468"}, traceOutputMatch{TMS: 40, EventStart: 3, EventEnd: 3}},
		{"regex", []string{"--regex", `ready\x1b\[\?2004h`}, traceOutputMatch{TMS: 40, EventStart: 3, EventEnd: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"trace", "contains-output", bundle}, test.args...)
			out, err := exec.Command(bin, args...).Output()
			if err != nil {
				t.Fatalf("contains-output: %v\n%s", err, out)
			}
			var got struct {
				OK   bool             `json:"ok"`
				Data traceOutputMatch `json:"data"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("decode output: %v\n%s", err, out)
			}
			if !got.OK {
				t.Fatalf("output = %s", out)
			}
			if got.Data != test.want {
				t.Fatalf("match = %+v, want %+v", got.Data, test.want)
			}
		})
	}
}

func TestTraceContainsOutputNoMatchIsAssertionFailure(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeTraceOutputBundle(t)
	out, err := exec.Command(bin, "trace", "contains-output", bundle, "--text", "absent").Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("error = %v, want exit 1; output %s", err, out)
	}
	var got struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Path        string `json:"path"`
				PatternKind string `json:"pattern_kind"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if got.OK || got.Error.Code != rpc.CodeAssertionFailed || got.Error.Details.Path != bundle || got.Error.Details.PatternKind != "text" {
		t.Fatalf("response = %+v", got)
	}
}

func TestTraceContainsOutputUsageErrors(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeTraceOutputBundle(t)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"missing matcher", []string{bundle}, "exactly one"},
		{"multiple matchers", []string{bundle, "--text", "x", "--hex", "78"}, "exactly one"},
		{"duplicate matcher", []string{bundle, "--text", "x", "--text", "y"}, "duplicate --text"},
		{"empty text", []string{bundle, "--text", ""}, "must not be empty"},
		{"empty hex", []string{bundle, "--hex", ""}, "must not be empty"},
		{"empty regex", []string{bundle, "--regex", ""}, "must not be empty"},
		{"bad hex", []string{bundle, "--hex", "xyz"}, "invalid --hex"},
		{"bad regex", []string{bundle, "--regex", "("}, "invalid --regex"},
		{"zero width regex", []string{bundle, "--regex", "a*"}, "consume at least one byte"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"trace", "contains-output"}, test.args...)
			cmd := exec.Command(bin, args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 {
				t.Fatalf("error = %v, want exit 2; stderr %s", err, &stderr)
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr missing %q:\n%s", test.want, &stderr)
			}
		})
	}
}

func TestTraceContainsOutputMachineUsageError(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeTraceOutputBundle(t)
	out, err := exec.Command(bin, "--machine", "trace", "contains-output", bundle).Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("error = %v, want exit 2; output %s", err, out)
	}
	if code := inspectErrorCode(t, out); code != rpc.CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", code, rpc.CodeInvalidArgument)
	}
}

func TestTraceContainsOutputBundleErrors(t *testing.T) {
	bin := buildBinary(t)
	missing := filepath.Join(t.TempDir(), "missing.twee")
	out, err := exec.Command(bin, "trace", "contains-output", missing, "--text", "x").Output()
	if err == nil || inspectErrorCode(t, out) != rpc.CodeIO {
		t.Fatalf("missing bundle error = %v output = %s", err, out)
	}

	invalid := writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{"version":2}`,
		"events.jsonl":  `{bad`,
	})
	out, err = exec.Command(bin, "trace", "contains-output", invalid, "--text", "x").Output()
	if err == nil || inspectErrorCode(t, out) != rpc.CodeInvalidArgument {
		t.Fatalf("invalid bundle error = %v output = %s", err, out)
	}
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil || !strings.HasPrefix(response.Error.Message, "trace contains-output:") {
		t.Fatalf("invalid bundle response = %s, decode error %v", out, err)
	}
	issues := inspectErrorIssues(t, out)
	if len(issues) != 2 || !containsInspectIssue(issues, "unsupported bundle version") || !containsInspectIssue(issues, "events.jsonl line 1") {
		t.Fatalf("issues = %v", issues)
	}
}

func TestTraceContainsOutputRegexRejectsInvalidUTF8(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":80,"rows":24}`,
		"events.jsonl":  `{"t_ms":10,"type":"output","bytes_b64":"/w=="}`,
	})

	if out, err := exec.Command(bin, "trace", "contains-output", bundle, "--hex", "ff").Output(); err != nil {
		t.Fatalf("hex query: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "trace", "contains-output", bundle, "--regex", "�").Output()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("regex error = %v, want exit 1; output %s", err, out)
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, out)
	}
	if response.Error.Code != rpc.CodeInvalidArgument || !strings.Contains(response.Error.Message, "requires valid UTF-8 output") {
		t.Fatalf("response = %+v", response)
	}
}

func TestTraceContainsOutputRejectsGlobalName(t *testing.T) {
	bin := buildBinary(t)
	bundle := writeTraceOutputBundle(t)
	cmd := exec.Command(bin, "--name", "session", "trace", "contains-output", bundle, "--text", "ready")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), "global --name is not valid") {
		t.Fatalf("error = %v stderr = %s", err, &stderr)
	}
}

func TestTraceContainsOutputHelpDescriptor(t *testing.T) {
	bin := buildBinary(t)
	out, err := exec.Command(bin, "help", "trace", "contains-output", "--format", "json").Output()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, out)
	}
	var got struct {
		Command struct {
			Path       []string          `json:"path"`
			Formats    []string          `json:"formats"`
			ExitStatus map[string]string `json:"exit_status"`
			Artifact   json.RawMessage   `json:"artifact"`
		} `json:"command"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode help: %v\n%s", err, out)
	}
	wantExit1 := "no output match (ASSERTION_FAILED), invalid bundle, or operational failure"
	if strings.Join(got.Command.Path, " ") != "trace contains-output" || len(got.Command.Formats) != 1 || got.Command.Formats[0] != "json" || got.Command.ExitStatus["1"] != wantExit1 || len(got.Command.Artifact) != 0 {
		t.Fatalf("descriptor = %+v", got.Command)
	}
}

func writeTraceOutputBundle(t *testing.T) string {
	t.Helper()
	encode := func(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
	return writeInspectRawBundle(t, map[string]string{
		"manifest.json": `{"version":1,"cols":80,"rows":24}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":10,"type":"output","bytes_b64":"` + encode([]byte{0xc3}) + `"}`,
			`{"t_ms":20,"type":"input","kind":"key","key":"Enter","bytes_b64":"DQ=="}`,
			`{"t_ms":30,"type":"resize","cols":100,"rows":40}`,
			`{"t_ms":40,"type":"output","bytes_b64":"` + encode(append([]byte{0xa9}, []byte(" ready\x1b[?2004h")...)) + `"}`,
		}, "\n"),
	})
}

// Keep syntax parsing in one place so the emptiness test uses the same mode as
// runTraceContainsOutput without coupling the table to parser details.
func syntaxParse(expr string) (*syntax.Regexp, error) {
	return syntax.Parse(expr, syntax.Perl)
}
