// Package engine owns the shared TUI-under-PTY runtime. It is consumed
// by both the tuitest Go test API and the cmd/twee daemon.
package engine

import (
	"os"
	"time"
)

// Config configures a Term. Callers populate fields directly; defaults
// are applied by Start when zero.
type Config struct {
	Cmd        []string
	Env        map[string]string // overrides on top of defaults
	Dir        string
	Cols, Rows int

	DefaultTimeout    time.Duration
	StableQuietWindow time.Duration

	RecordPath string
	TracePath  string
}

// applyDefaults fills in zero fields with sensible values.
func (c *Config) applyDefaults() {
	if c.Cols == 0 {
		c.Cols = 80
	}
	if c.Rows == 0 {
		c.Rows = 24
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 5 * time.Second
	}
	if c.StableQuietWindow == 0 {
		c.StableQuietWindow = 100 * time.Millisecond
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
}

// BuildEnv assembles the final []string env for exec, applying TERM/
// COLORTERM/LANG defaults and inheriting PATH/HOME/USER from the parent
// when not overridden.
func (c *Config) BuildEnv() []string {
	defaults := map[string]string{
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		"LANG":      "C.UTF-8",
	}
	for k, v := range c.Env {
		defaults[k] = v
	}
	for _, k := range []string{"PATH", "HOME", "USER"} {
		if _, ok := defaults[k]; !ok {
			if v := os.Getenv(k); v != "" {
				defaults[k] = v
			}
		}
	}
	out := make([]string, 0, len(defaults))
	for k, v := range defaults {
		out = append(out, k+"="+v)
	}
	return out
}
