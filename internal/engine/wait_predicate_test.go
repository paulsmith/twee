package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWaitForCellTimeoutDiagnosticUsesEvaluatedSnapshot(t *testing.T) {
	term, err := Start(context.Background(), Config{
		Cmd:  []string{"/bin/sh", "-c", "printf X; sleep 0.02; while :; do printf '\\rA'; sleep 0.002; printf '\\rB'; sleep 0.002; done"},
		Cols: 4,
		Rows: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.WaitForText("X", WithTimeout(time.Second)); err != nil {
		t.Fatal(err)
	}

	never := "never"
	snapshot, err := term.WaitForCellAtSnapshot(0, 0, CellPredicate{Text: &never}, WithTimeout(100*time.Millisecond))
	if err == nil {
		t.Fatal("WaitForCellAtSnapshot unexpectedly succeeded")
	}
	want := "--- visible screen ---\n" + VisibleSnapshotText(snapshot) + "\n--- recent input events"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("diagnostic does not describe evaluated snapshot\nerror:\n%s\nsnapshot:\n%s", err, VisibleSnapshotText(snapshot))
	}
}
