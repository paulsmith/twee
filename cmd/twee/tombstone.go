package main

import (
	"encoding/json"
	"os"
	"time"
)

// tombstone records how a session ended: naturally (the child exited on
// its own) or via an explicit "twee stop". The daemon writes it to
// "<name>.exited" in the state dir just before removing its socket and
// lock file, so a script that only checks in later can still tell "ran
// and finished" from "never existed" without having watched the session
// die.
type tombstone struct {
	Name string `json:"name"`
	// ExitCode is nil when the child was terminated by a signal instead
	// of exiting normally; Signal names that signal in that case.
	ExitCode      *int      `json:"exit_code"`
	Signal        string    `json:"signal,omitempty"`
	Stopped       bool      `json:"stopped"`
	StoppedAt     time.Time `json:"stopped_at"`
	Command       []string  `json:"command"`
	TracePath     string    `json:"trace_path,omitempty"`
	ArtifactError string    `json:"artifact_error,omitempty"`
}

// writeTombstone best-effort records name's tombstone. Errors are
// swallowed: a missing tombstone just falls back to the pre-existing
// NOT_FOUND behavior later, no worse than before this feature existed.
// Written to a temp file and renamed into place so a concurrent reader
// (see readTombstone) never observes a half-written file.
func writeTombstone(name string, ts tombstone) {
	path, err := tombstonePath(name)
	if err != nil {
		return
	}
	raw, err := json.Marshal(ts)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func writeTombstoneForGeneration(name, token string, ts tombstone) {
	metadata, ok := readLockMetadata(name)
	if !ok || metadata.Token != token {
		return
	}
	writeTombstone(name, ts)
}

// readTombstone loads name's tombstone, if any. A missing file, or one
// that fails to parse, is treated as "no tombstone" rather than an
// error: two twee processes can race on tombstone read/remove (a
// "start" clearing an old one while a "status" reads it, say), and a
// half-written or already-gone file from that race must never crash a
// reader — it should just look like the tombstone was never there.
func readTombstone(name string) (tombstone, bool) {
	path, err := tombstonePath(name)
	if err != nil {
		return tombstone{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return tombstone{}, false
	}
	var ts tombstone
	if err := json.Unmarshal(raw, &ts); err != nil {
		return tombstone{}, false
	}
	return ts, true
}

// removeTombstone best-effort removes name's leftover tombstone. Called
// by a fresh "twee start" so an old session's exit info doesn't linger
// around to be confused with the new one under the same name.
func removeTombstone(name string) {
	if path, err := tombstonePath(name); err == nil {
		_ = os.Remove(path)
	}
}
