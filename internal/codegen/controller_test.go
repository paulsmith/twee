package codegen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptControllerIsOneShot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.json")
	c := &scriptController{}
	if err := c.start(path, 80, 24, true); err != nil {
		t.Fatal(err)
	}
	if !c.partial || c.state != recorderRecording {
		t.Fatalf("state = %+v", c)
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := c.start(filepath.Join(t.TempDir(), "again.json"), 80, 24, false); err == nil {
		t.Fatal("restart succeeded")
	}
}

func TestStatusUsesOneSpinnerPhase(t *testing.T) {
	s := statusBar{phase: 3}
	if s.mark(recorderRecording) != spinnerFrames[3] {
		t.Fatalf("mark = %q", s.mark(recorderRecording))
	}
	if got := truncateStatus("abcdef", 4); got != "abc." {
		t.Fatalf("truncate = %q", got)
	}
}

func TestStatusLineHonorsDisabledStatus(t *testing.T) {
	var out bytes.Buffer
	status := statusBar{w: &out, enabled: false, rows: 24, cols: 80}
	status.drawLine(status.markerPrompt("secret"))
	if out.Len() != 0 {
		t.Fatalf("disabled status line wrote %q", out.String())
	}
}

func TestSanitizeStatusPreventsControlsAndASCII(t *testing.T) {
	got := sanitizeStatus("x\x1b[2J\n界", true)
	if got != "x [2J ?" {
		t.Fatalf("status=%q", got)
	}
}

func TestStatusCellTruncationAndOutcome(t *testing.T) {
	if got := truncateStatus("界a", 2); got != "." {
		t.Fatalf("wide truncate=%q", got)
	}
	if got := truncateStatus("a界", 2); got != "a." {
		t.Fatalf("truncate=%q", got)
	}
	if statusWidth("e\u0301") != 1 {
		t.Fatal("combining width")
	}
	s := &scriptController{state: recorderFinalized, path: "x", partial: true}
	tr := &traceController{state: recorderFinalized, path: "y"}
	if got := artifactSummary(s, tr); got != "script saved: x (partial); trace saved: y" {
		t.Fatalf("summary=%q", got)
	}
	if got := artifactSummary(&scriptController{}, &traceController{}); got != "" {
		t.Fatalf("summary=%q", got)
	}
}

func TestStatusKeepsHintsAheadOfLongPaths(t *testing.T) {
	s := statusBar{}
	script := &scriptController{state: recorderRecording, partial: true, path: "bad\x1b[2J" + strings.Repeat("x", 200)}
	trace := &traceController{state: recorderRecording, path: strings.Repeat("y", 200)}
	line := truncateStatus(s.line(script, trace), 80)
	for _, hint := range []string{"^]q", "^]s", "^]t", "^]m", "partial"} {
		if !strings.Contains(line, hint) {
			t.Fatalf("status missing %q: %q", hint, line)
		}
	}
	if strings.Contains(line, "\x1b") {
		t.Fatalf("status contains control: %q", line)
	}
}

func TestTerminalPathQuotesControls(t *testing.T) {
	path := "report\x1b[2J\nnext"
	if got := terminalPath(path); strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\n') {
		t.Fatalf("unsafe path feedback: %q", got)
	}
	s := artifactSummary(&scriptController{state: recorderFinalized, path: path}, &traceController{})
	if strings.ContainsRune(s, '\x1b') || strings.ContainsRune(s, '\n') {
		t.Fatalf("unsafe summary: %q", s)
	}
}

func TestCompositorCapability(t *testing.T) {
	for _, tc := range []struct {
		no        bool
		term      string
		tty, want bool
	}{{true, "xterm", true, false}, {false, "dumb", true, false}, {false, "xterm", false, false}, {false, "xterm", true, true}} {
		if got := compositorCapable(tc.no, tc.term, tc.tty); got != tc.want {
			t.Fatalf("capability=%v", got)
		}
	}
}
