package tuitest

import (
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
)

// Option configures a Term.
type Option func(*config)

type config struct {
	cmd        []string
	extraArgs  []string
	env        map[string]string
	dir        string
	cols, rows int

	defaultTimeout    time.Duration
	stableQuietWindow time.Duration

	recordPath string
	tracePath  string
}

func newConfig() *config {
	return &config{
		cols:              80,
		rows:              24,
		defaultTimeout:    5 * time.Second,
		stableQuietWindow: 100 * time.Millisecond,
		env:               map[string]string{},
	}
}

func (c *config) toEngine() engine.Config {
	return engine.Config{
		Cmd:               c.cmd,
		Env:               c.env,
		Dir:               c.dir,
		Cols:              c.cols,
		Rows:              c.rows,
		DefaultTimeout:    c.defaultTimeout,
		StableQuietWindow: c.stableQuietWindow,
		RecordPath:        c.recordPath,
		TracePath:         c.tracePath,
	}
}

// Command sets the command to run.
func Command(args ...string) Option {
	return func(c *config) { c.cmd = append([]string{}, args...) }
}

// Args appends arguments after the command.
func Args(args ...string) Option {
	return func(c *config) { c.extraArgs = append(c.extraArgs, args...) }
}

// Size sets the initial terminal size.
func Size(cols, rows int) Option {
	return func(c *config) { c.cols, c.rows = cols, rows }
}

// Env sets a single environment variable.
func Env(key, value string) Option {
	return func(c *config) { c.env[key] = value }
}

// Dir sets the working directory of the child.
func Dir(dir string) Option {
	return func(c *config) { c.dir = dir }
}

// DefaultTimeout sets the default timeout for WaitFor* and Expect*.
func DefaultTimeout(d time.Duration) Option {
	return func(c *config) { c.defaultTimeout = d }
}

// Record enables session recording to the given path.
func Record(path string) Option {
	return func(c *config) { c.recordPath = path }
}

// Trace enables trace recording to the given path. The trace is a .twee
// zip bundle containing a manifest, JSONL event stream, and screenshots.
func Trace(path string) Option {
	return func(c *config) { c.tracePath = path }
}
