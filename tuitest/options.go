package tuitest

import (
	"time"

	"github.com/paulsmith/twee/internal/engine"
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

	tracePath string
	network   bool
	publish   []TCPPublication
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
	var wholeSessionTrace *engine.WholeSessionTraceConfig
	if c.tracePath != "" || c.network {
		wholeSessionTrace = &engine.WholeSessionTraceConfig{Path: c.tracePath}
		if c.network {
			publications := make([]engine.TCPPublication, len(c.publish))
			for i, publication := range c.publish {
				publications[i] = engine.TCPPublication{Listen: publication.Listen, Guest: publication.Guest}
			}
			wholeSessionTrace.Network = &engine.NetworkCaptureConfig{PublishTCP: publications}
		}
	}
	return engine.Config{
		Cmd:               c.cmd,
		Env:               c.env,
		Dir:               c.dir,
		Cols:              c.cols,
		Rows:              c.rows,
		DefaultTimeout:    c.defaultTimeout,
		StableQuietWindow: c.stableQuietWindow,
		WholeSessionTrace: wholeSessionTrace,
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

// Record enables .twee trace recording to the given path.
func Record(path string) Option {
	return func(c *config) { c.tracePath = path }
}

// Trace enables trace recording to the given path. The trace is a .twee
// zip bundle containing a manifest and JSONL event stream.
func Trace(path string) Option {
	return func(c *config) { c.tracePath = path }
}

// TCPPublication maps a host TCP listener to an address in the managed
// program's private network. Guest must be 10.0.2.100:PORT.
type TCPPublication struct {
	Listen string
	Guest  string
}

// NetworkCapture records the managed program's IPv4 traffic in the .twee
// bundle at path. Publications let host clients reach servers in the private
// network. Network capture is supported on Linux only.
func NetworkCapture(path string, publications ...TCPPublication) Option {
	return func(c *config) {
		c.tracePath = path
		c.network = true
		c.publish = append([]TCPPublication(nil), publications...)
	}
}
