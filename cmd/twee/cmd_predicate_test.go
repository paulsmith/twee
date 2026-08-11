package main

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestExtractCellPredicateArgsPreservesTriStateAndOtherFlags(t *testing.T) {
	remaining, predicate, err := extractCellPredicateArgs([]string{
		"--x", "0", "--y", "1", "--text=", "--width", "0",
		"--fg", "palette:1", "--bg=#001020", "--bold=false", "--underline",
		"--timeout", "2s",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--x", "0", "--y", "1", "--timeout", "2s"}; !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining = %q, want %q", remaining, want)
	}
	if predicate.Text == nil || *predicate.Text != "" || predicate.Width == nil || *predicate.Width != 0 {
		t.Fatalf("text/width predicate = %+v", predicate)
	}
	if predicate.Fg == nil || predicate.Fg.Kind != rpc.ColorKindPalette || predicate.Fg.Index == nil || *predicate.Fg.Index != 1 {
		t.Fatalf("fg predicate = %+v", predicate.Fg)
	}
	if predicate.Bg == nil || predicate.Bg.R == nil || *predicate.Bg.R != 0 || predicate.Bg.G == nil || *predicate.Bg.G != 0x10 || predicate.Bg.B == nil || *predicate.Bg.B != 0x20 {
		t.Fatalf("bg predicate = %+v", predicate.Bg)
	}
	if predicate.Bold == nil || *predicate.Bold || predicate.Underline == nil || !*predicate.Underline {
		t.Fatalf("style predicate = %+v", predicate)
	}
}

func TestContainsStyleUsesSharedPredicateFields(t *testing.T) {
	remaining, predicate, err := extractCellPredicateArgs([]string{
		"--contains-style", "fg=palette:1",
		"--contains-style=bold=false",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 || predicate.Fg == nil || predicate.Bold == nil || *predicate.Bold {
		t.Fatalf("remaining=%q predicate=%+v", remaining, predicate)
	}
}

func TestExtractCellPredicateArgsStopsAtOptionBoundary(t *testing.T) {
	remaining, predicate, err := extractCellPredicateArgs([]string{
		"--text", "X", "--", "--bold=false",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--", "--bold=false"}; !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining = %q, want %q", remaining, want)
	}
	if predicate.Text == nil || *predicate.Text != "X" || predicate.Bold != nil {
		t.Fatalf("predicate = %+v, want text only", predicate)
	}
}

func TestExtractCellPredicateArgsRejectsInvalidAndDuplicateFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--fg", "red"},
		{"--width", "wide"},
		{"--width", "3"},
		{"--bold=maybe"},
		{"--bold", "--bold=false"},
		{"--contains-style", "blink=true"},
	} {
		if _, _, err := extractCellPredicateArgs(args, true); err == nil {
			t.Errorf("extractCellPredicateArgs(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParsePredicateColorForms(t *testing.T) {
	for _, test := range []struct {
		value string
		kind  string
	}{
		{"default", rpc.ColorKindDefault},
		{"palette:0", rpc.ColorKindPalette},
		{"#00ff10", rpc.ColorKindRGB},
		{"rgb:0,255,16", rpc.ColorKindRGB},
	} {
		color, err := parsePredicateColor(test.value)
		if err != nil || color.Kind != test.kind {
			t.Errorf("parsePredicateColor(%q) = %+v, %v", test.value, color, err)
		}
	}
}

func TestPredicateCLIRejectsInvalidGrammar(t *testing.T) {
	bin := buildBinary(t)
	env := testEnv(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"duplicate coordinate", []string{"wait", "cell", "--x", "0", "--x=1", "--y", "0", "--text", "x"}, "duplicate --x"},
		{"empty predicate", []string{"assert", "cell", "--x", "0", "--y", "0"}, "at least one cell predicate is required"},
		{"negative coordinate", []string{"assert", "cell", "--x=-1", "--y", "0", "--text", "x"}, "x and y must be >= 0"},
		{"partial region", []string{"assert", "region", "--x", "0", "--text", "x"}, "x, y, w, and h must be provided together"},
		{"invalid region size", []string{"assert", "region", "--x", "0", "--y", "0", "--w", "0", "--h", "1", "--text", "x"}, "w/h must be > 0"},
		{"invalid match", []string{"assert", "region", "--match", "some", "--text", "x"}, "--match must be any or all"},
		{"option boundary", []string{"assert", "cell", "--x", "0", "--y", "0", "--text", "x", "--", "--bold=false"}, "too many positional arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, output, err := runCLI(t, bin, env, test.args...)
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 2 {
				t.Fatalf("error = %v, want exit code 2; output: %s", err, output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("output = %q, want substring %q", output, test.want)
			}
		})
	}
}
