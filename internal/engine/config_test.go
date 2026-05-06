package engine

import (
	"os"
	"testing"
)

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
