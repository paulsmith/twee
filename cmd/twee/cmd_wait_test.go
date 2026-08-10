package main

import (
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestParseWaitExclude(t *testing.T) {
	got, err := parseWaitExclude("2,3,4,5")
	if err != nil {
		t.Fatalf("parseWaitExclude: %v", err)
	}
	if want := (rpc.Rect{X: 2, Y: 3, W: 4, H: 5}); got != want {
		t.Fatalf("parseWaitExclude = %+v, want %+v", got, want)
	}
	for _, value := range []string{"", "1,2,3", "1,2,3,4,5", "x,2,3,4", "-1,2,3,4", "1,-2,3,4", "1,2,0,4", "1,2,3,0"} {
		if _, err := parseWaitExclude(value); err == nil {
			t.Errorf("parseWaitExclude(%q) unexpectedly succeeded", value)
		}
	}
}

func TestParseWaitStableRepeatedExclude(t *testing.T) {
	remaining, got, err := extractWaitExcludes([]string{"--quiet", "10ms", "--exclude", "0,1,2,3", "--exclude=4,5,6,7"})
	if err != nil {
		t.Fatalf("extractWaitExcludes: %v", err)
	}
	if want := []string{"0,1,2,3", "4,5,6,7"}; !sameStrings(got, want) {
		t.Fatalf("exclude = %#v, want %#v", got, want)
	}
	if want := []string{"--quiet", "10ms"}; !sameStrings(remaining, want) {
		t.Fatalf("remaining = %#v, want %#v", remaining, want)
	}
	if _, _, err := extractWaitExcludes([]string{"--exclude"}); err == nil {
		t.Fatal("missing --exclude value unexpectedly succeeded")
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
