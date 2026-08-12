package play

import (
	"strings"
	"testing"
)

func TestReadCommandsMapsStatusToggleAndSpeed(t *testing.T) {
	out := make(chan command, 4)
	readCommands(strings.NewReader("h-+q"), out)

	for i, want := range []command{cmdToggleStatus, cmdSlower, cmdFaster, cmdQuit} {
		if got := <-out; got != want {
			t.Fatalf("command %d = %v, want %v", i, got, want)
		}
	}
	if _, ok := <-out; ok {
		t.Fatal("command channel remains open")
	}
}
