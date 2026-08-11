package engine

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDiagnosticScreenTextIsBounded(t *testing.T) {
	cells := make([]Cell, 70*1024)
	for i := range cells {
		cells[i] = Cell{Text: "界", Width: 1}
	}
	snapshot := Snapshot{Cols: len(cells), Rows: 1, Lines: []Line{{Cells: cells}}}
	text, total, truncated := DiagnosticScreenText(snapshot)
	if !truncated || len(text) > 64*1024 || total <= len(text) {
		t.Fatalf("screen bounds = len %d total %d truncated %v", len(text), total, truncated)
	}
	if !utf8.ValidString(text) {
		t.Fatal("bounded screen split UTF-8")
	}
}

func TestDiagnosticStringRedactsAndBoundsInputDescriptions(t *testing.T) {
	secret := strings.Repeat("secret", 1<<20)
	diagnostic := Diagnostic{
		Snapshot: Snapshot{Cols: 1, Rows: 1, Lines: []Line{{Cells: []Cell{{Width: 1}}}}},
		RecentInputs: []InputEvent{
			{Kind: "type", Desc: "Type " + secret},
			{Kind: "key", Desc: strings.Repeat("K", 1<<20)},
		},
	}
	rendered := diagnostic.String()
	if strings.Contains(rendered, secret[:1024]) {
		t.Fatal("diagnostic exposed typed payload")
	}
	if !strings.Contains(rendered, "type payload redacted") {
		t.Fatalf("diagnostic missing redaction marker: %s", rendered)
	}
	if len(rendered) > 16*1024 {
		t.Fatalf("diagnostic length = %d, want bounded output", len(rendered))
	}
}

func TestWaitForExitTimeoutNeverReportsExitedDiagnostic(t *testing.T) {
	for range 20 {
		term, err := Start(context.Background(), Config{
			Cmd: []string{"/bin/sh", "-c", "sleep 0.005"}, Cols: 2, Rows: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, waitErr := term.WaitForExit(WithTimeout(5 * time.Millisecond))
		if waitErr != nil {
			diagnostic, ok := DiagnosticFromError(waitErr)
			if !ok {
				t.Fatalf("error lacks diagnostic: %v", waitErr)
			}
			if diagnostic.ExitCode != nil || strings.Contains(waitErr.Error(), "exit status: 0") {
				t.Fatalf("timeout contradicted by exit status: %v", waitErr)
			}
		}
		_ = term.Close()
	}
}
