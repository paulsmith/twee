package main

import (
	"errors"
	"os"
	"testing"
)

func TestSessionNamePrecedenceAndValidation(t *testing.T) {
	env := func(key string) (string, bool) {
		if key != envClientSession {
			return "", false
		}
		return "from-env", true
	}

	got, err := sessionName(nameOpt{Value: "local", Present: true}, nameOpt{Value: "global", Present: true}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != "local" {
		t.Fatalf("name = %q, want local", got)
	}

	got, err = sessionName(nameOpt{}, nameOpt{Value: "global", Present: true}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != "global" {
		t.Fatalf("name = %q, want global", got)
	}

	got, err = sessionName(nameOpt{}, nameOpt{}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Fatalf("name = %q, want from-env", got)
	}

	got, err = sessionName(nameOpt{}, nameOpt{}, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if got != "default" {
		t.Fatalf("name = %q, want default", got)
	}

	for _, tt := range []struct {
		name  string
		local nameOpt
	}{
		{name: "empty explicit", local: nameOpt{Present: true}},
		{name: "dash leading", local: nameOpt{Value: "-bad", Present: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sessionName(tt.local, nameOpt{}, func(string) (string, bool) { return "", false })
			if err == nil {
				t.Fatal("sessionName unexpectedly succeeded")
			}
		})
	}
}

func TestResolveSessionNamePtrPrefersLocalName(t *testing.T) {
	oldGlobal := rootGlobalName
	t.Cleanup(func() { rootGlobalName = oldGlobal })
	rootGlobalName = nameOpt{Value: "global", Present: true}

	local := "local"
	got, err := resolveSessionNamePtr(&local)
	if err != nil {
		t.Fatal(err)
	}
	if got != "local" {
		t.Fatalf("session name = %q, want local", got)
	}
}

func TestResolveSessionNamePtrRejectsInvalidName(t *testing.T) {
	local := "../bad"
	_, err := resolveSessionNamePtr(&local)
	if err == nil {
		t.Fatal("resolveSessionNamePtr unexpectedly accepted invalid name")
	}
}

func TestParseRootArgsGlobalNameAndHelp(t *testing.T) {
	root, err := parseRootArgs([]string{"--name", "s", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if root.Verb != "status" || root.GlobalName.Value != "s" || !root.GlobalName.Present {
		t.Fatalf("root = %+v", root)
	}
	if len(root.Args) != 0 {
		t.Fatalf("args = %#v, want empty", root.Args)
	}

	root, err = parseRootArgs([]string{"wait", "text", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	if !root.Help || root.HelpKey != "wait text" {
		t.Fatalf("root help = %+v, want wait text help", root)
	}

	root, err = parseRootArgs([]string{"type", "--", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	if root.Help {
		t.Fatalf("post-boundary --help was treated as help: %+v", root)
	}

	if _, err := parseRootArgs([]string{"-h"}); !errors.Is(err, errShortOption) {
		t.Fatalf("parseRootArgs(-h) err = %v, want errShortOption", err)
	}
	if _, err := parseRootArgs([]string{"--name", "s", "run"}); err == nil {
		t.Fatal("global --name for run unexpectedly succeeded")
	}
	for _, verb := range []string{"click", "hover", "scroll", "drag"} {
		root, err := parseRootArgs([]string{"--name", "s", verb})
		if err != nil {
			t.Fatalf("global --name for %s: %v", verb, err)
		}
		if !root.GlobalName.Present || root.GlobalName.Value != "s" {
			t.Fatalf("global --name for %s not retained: %+v", verb, root)
		}
	}
}

func TestRejectShortOptionsBeforeBoundary(t *testing.T) {
	for _, args := range [][]string{
		{"-name", "s"},
		{"--name", "s", "-regex"},
		{"--help", "-h"},
	} {
		if err := rejectShortOptions(args); !errors.Is(err, errShortOption) {
			t.Fatalf("rejectShortOptions(%#v) = %v, want errShortOption", args, err)
		}
	}
	if err := rejectShortOptions([]string{"--", "-name", "-h"}); err != nil {
		t.Fatalf("post-boundary short-looking payload rejected: %v", err)
	}
}

func TestSplitExplicitBoundary(t *testing.T) {
	before, after, err := splitExplicitBoundary("run", []string{"--script", "ops.json", "--", "/bin/echo", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	if got := append(append([]string{}, before...), after...); len(got) != 4 {
		t.Fatalf("before=%#v after=%#v", before, after)
	}
	if after[0] != "/bin/echo" || after[1] != "--help" {
		t.Fatalf("after = %#v", after)
	}
	if _, _, err := splitExplicitBoundary("run", []string{"/bin/echo"}); err == nil {
		t.Fatal("missing boundary unexpectedly succeeded")
	}
}

func TestTWEESESSIONIsNotReadByLocalCommands(t *testing.T) {
	t.Setenv(envClientSession, "ignored")
	root, err := parseRootArgs([]string{"sleep", "1ms"})
	if err != nil {
		t.Fatal(err)
	}
	if root.GlobalName.Present {
		t.Fatalf("root = %+v", root)
	}
	if got := os.Getenv(envClientSession); got != "ignored" {
		t.Fatalf("test env changed to %q", got)
	}
}
