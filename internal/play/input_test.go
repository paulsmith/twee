package play

import (
	"strings"
	"testing"
)

func TestReadCommandsMapsStatusToggle(t *testing.T) {
	out := make(chan command, 2)
	readCommands(strings.NewReader("hq"), out)

	if got := <-out; got != cmdToggleStatus {
		t.Fatalf("first command = %v, want toggle status", got)
	}
	if got := <-out; got != cmdQuit {
		t.Fatalf("second command = %v, want quit", got)
	}
	if _, ok := <-out; ok {
		t.Fatal("command channel remains open")
	}
}
