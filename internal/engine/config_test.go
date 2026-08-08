package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatTCPPublicationHidesGuestAddress(t *testing.T) {
	got, err := FormatTCPPublication(TCPPublication{
		Listen: "127.0.0.1:8080", Guest: "10.0.2.100:3000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:8080=3000" {
		t.Fatalf("publication = %q, want simplified form", got)
	}
}

func TestFormatTCPPublicationRejectsInvalidGuestAddress(t *testing.T) {
	for _, guest := range []string{"", "10.0.2.100", "10.0.2.100:http", "10.0.2.100:0", "10.0.2.100:65536"} {
		if _, err := FormatTCPPublication(TCPPublication{Listen: "127.0.0.1:8080", Guest: guest}); err == nil {
			t.Errorf("guest %q unexpectedly succeeded", guest)
		}
	}
}

func TestBuildEnvInheritsParentAndAppliesOverrides(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/codex-home")
	t.Setenv("TWEE_PARENT_ENV_TEST", "parent")

	env := (&Config{Env: map[string]string{
		"TWEE_PARENT_ENV_TEST": "override",
		"TWEE_CHILD_ENV_TEST":  "child",
	}}).BuildEnv()

	got := envMap(env)
	if got["CODEX_HOME"] != "/tmp/codex-home" {
		t.Fatalf("CODEX_HOME = %q, want inherited value", got["CODEX_HOME"])
	}
	if got["TWEE_PARENT_ENV_TEST"] != "override" {
		t.Fatalf("TWEE_PARENT_ENV_TEST = %q, want override", got["TWEE_PARENT_ENV_TEST"])
	}
	if got["TWEE_CHILD_ENV_TEST"] != "child" {
		t.Fatalf("TWEE_CHILD_ENV_TEST = %q, want child", got["TWEE_CHILD_ENV_TEST"])
	}
}

func TestStartRejectsInvalidWholeSessionTraceConfig(t *testing.T) {
	_, err := Start(context.Background(), Config{
		Cmd: []string{"/bin/true"}, WholeSessionTrace: &WholeSessionTraceConfig{},
	})
	if err == nil || !strings.Contains(err.Error(), "trace path is empty") {
		t.Fatalf("Start error = %v, want empty whole-session trace path", err)
	}
}

func TestWholeSessionTraceCannotBeReconfiguredMidSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.twee")
	term, err := Start(context.Background(), Config{
		Cmd:               []string{"/bin/sh", "-c", "sleep 30"},
		WholeSessionTrace: &WholeSessionTraceConfig{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.EnableTrace(filepath.Join(t.TempDir(), "replacement.twee")); err == nil {
		t.Fatal("EnableTrace replaced whole-session trace")
	}
	if err := term.DisableTrace(); err == nil {
		t.Fatal("DisableTrace stopped whole-session trace")
	}
}

func TestBuildEnvAddsTerminalDefaultsWhenMissing(t *testing.T) {
	restoreEnv(t, "TERM")
	restoreEnv(t, "COLORTERM")
	restoreEnv(t, "LANG")
	if err := os.Unsetenv("TERM"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("COLORTERM"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("LANG"); err != nil {
		t.Fatal(err)
	}

	got := envMap((&Config{}).BuildEnv())
	if got["TERM"] != "xterm-256color" {
		t.Fatalf("TERM = %q, want xterm-256color", got["TERM"])
	}
	if got["COLORTERM"] != "truecolor" {
		t.Fatalf("COLORTERM = %q, want truecolor", got["COLORTERM"])
	}
	if got["LANG"] != "C.UTF-8" {
		t.Fatalf("LANG = %q, want C.UTF-8", got["LANG"])
	}
}

func restoreEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		for i, r := range kv {
			if r == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}
