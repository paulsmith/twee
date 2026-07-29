package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// stateDir returns the directory in which named-session sockets and
// lock files live. The directory is created with 0700 if missing.
//
// Override resolution order: TWEE_STATE_DIR (used for testing), then
// platform default (XDG_STATE_HOME or macOS Library path), then a
// $TMPDIR/twee-$USER fallback.
func stateDir() (string, error) {
	if v := os.Getenv("TWEE_STATE_DIR"); v != "" {
		if err := os.MkdirAll(v, 0o700); err != nil {
			return "", fmt.Errorf("stateDir: %w", err)
		}
		return v, nil
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		home := os.Getenv("HOME")
		if home != "" {
			base = filepath.Join(home, "Library", "Application Support")
		}
	default:
		base = os.Getenv("XDG_STATE_HOME")
		if base == "" {
			home := os.Getenv("HOME")
			if home != "" {
				base = filepath.Join(home, ".local", "state")
			}
		}
	}
	if base == "" {
		// Fallback: $TMPDIR/twee-$USER.
		tmp := os.Getenv("TMPDIR")
		if tmp == "" {
			tmp = "/tmp"
		}
		user := os.Getenv("USER")
		if user == "" {
			user = "default"
		}
		base = filepath.Join(tmp, "twee-"+user)
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", fmt.Errorf("stateDir: %w", err)
		}
		return base, nil
	}
	dir := filepath.Join(base, "twee")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("stateDir: %w", err)
	}
	return dir, nil
}

// socketPath returns the socket path for a named session.
func socketPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sock"), nil
}

// lockPath returns the lock-file path for a named session.
func lockPath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".lock"), nil
}

// tombstonePath returns the path of the tombstone file a session's
// daemon leaves behind recording how it ended (see tombstone.go).
func tombstonePath(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".exited"), nil
}

// absOutPath resolves a client-supplied path (an --out destination, or
// an input path like diff's --against) against the client's own working
// directory before it travels over the RPC wire. Without this, a
// relative path is later interpreted by the daemon process, which may
// have a different cwd than the client invoking this command (the
// daemon keeps whatever cwd it had at `start` time). Empty stays empty,
// meaning "let the daemon choose a default path" (only meaningful for
// --out flags; diff's --against is required and never empty).
func absOutPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", p, err)
	}
	return abs, nil
}

// validateName rejects names that are empty, contain path separators or
// NUL, look like traversal, or begin with "-".
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must be non-empty")
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("name must not begin with '-'")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("name must not contain path separators or NUL")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name %q is reserved", name)
	}
	return nil
}
