package tuitest

import (
	"strings"
	"testing"
)

func TestRequireTestingTBPanicsWithoutRun(t *testing.T) {
	var term Term
	defer func() {
		v := recover()
		if v == nil {
			t.Fatal("requireTestingTB unexpectedly returned")
		}
		if got := v.(string); !strings.Contains(got, "ExpectText requires Run(t, ...)") {
			t.Fatalf("panic = %q, want ExpectText Run hint", got)
		}
	}()

	term.requireTestingTB("ExpectText")
}
