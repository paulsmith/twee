// Package engine owns the shared TUI-under-PTY runtime. It is consumed
// by both the tuitest Go test API and the cmd/twee daemon.
package engine

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/paulsmith/twee/internal/networkcapture"
)

// Config configures a Term. Callers populate fields directly; defaults
// are applied by Start when zero.
type Config struct {
	Cmd        []string
	Env        map[string]string // overrides on top of parent environment and defaults
	Dir        string
	Cols, Rows int

	DefaultTimeout    time.Duration
	StableQuietWindow time.Duration

	// WholeSessionTrace starts recording before the command and finalizes it
	// only after the command and any network recorder have stopped.
	WholeSessionTrace *WholeSessionTraceConfig
}

// WholeSessionTraceConfig describes artifacts whose lifetime must match the
// launched command. Network capture cannot be enabled independently of it.
type WholeSessionTraceConfig struct {
	Path    string
	Network *NetworkCaptureConfig
}

// NetworkCaptureConfig enables managed network capture for a whole-session
// trace.
type NetworkCaptureConfig struct {
	PublishTCP []TCPPublication
}

// TCPPublication maps a host listener to a private guest address.
type TCPPublication = networkcapture.Publication

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

func (c Config) validate() error {
	if c.WholeSessionTrace != nil && c.WholeSessionTrace.Path == "" {
		return errors.New("engine.Start: whole-session trace path is empty")
	}
	return nil
}

// BuildEnv assembles the final []string env for exec, inheriting the parent
// environment, applying terminal defaults for missing values, and then applying
// explicit overrides.
func (c *Config) BuildEnv() []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	defaults := map[string]string{
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		"LANG":      "C.UTF-8",
	}
	for k, v := range defaults {
		if _, ok := env[k]; !ok {
			env[k] = v
		}
	}
	for k, v := range c.Env {
		env[k] = v
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(env))
	for _, k := range keys {
		v := env[k]
		out = append(out, k+"="+v)
	}
	return out
}
