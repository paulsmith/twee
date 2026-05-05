package codegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/trace"
	"github.com/paulsmith/research/twee/internal/vt"
)

type traceMode int

const (
	traceModeNone traceMode = iota
	traceModeFullSession
	traceModeHotkey
)

type traceController struct {
	command []string
	env     map[string]string
	cols    int
	rows    int
	pid     int
	outPath string
	stderr  io.Writer

	tr        *trace.Trace
	mode      traceMode
	path      string
	startedAt time.Time
}

func newTraceController(opts Options, cols, rows, pid int) *traceController {
	return &traceController{
		command: append([]string(nil), opts.Command...),
		env:     opts.Env,
		cols:    cols,
		rows:    rows,
		pid:     pid,
		outPath: opts.OutPath,
		stderr:  opts.Stderr,
	}
}

func (c *traceController) startFullSession(path string, snap vt.Snapshot) error {
	return c.start(path, traceModeFullSession, snap)
}

func (c *traceController) toggleHotkey(snap vt.Snapshot) error {
	switch c.mode {
	case traceModeFullSession:
		fmt.Fprintf(c.stderr, "\r\ntwee codegen: already tracing full session: %s\r\n", c.path)
		return nil
	case traceModeHotkey:
		path := c.path
		if err := c.close(); err != nil {
			return err
		}
		fmt.Fprintf(c.stderr, "\r\ntwee codegen: stopped trace recording: %s\r\n", path)
		return nil
	default:
		path, err := nextHotkeyTracePath(c.outPath, time.Now())
		if err != nil {
			return err
		}
		if err := c.start(path, traceModeHotkey, snap); err != nil {
			return err
		}
		fmt.Fprintf(c.stderr, "\r\ntwee codegen: started trace recording: %s\r\n", path)
		return nil
	}
}

func (c *traceController) start(path string, mode traceMode, snap vt.Snapshot) error {
	startedAt := time.Now()
	tr, err := trace.New(path, trace.Manifest{
		Command: c.command,
		Env:     c.env,
		Cols:    c.cols,
		Rows:    c.rows,
		Pid:     c.pid,
	})
	if err != nil {
		return err
	}
	if seed := engine.TraceSeedOutput(snap); len(seed) > 0 {
		tr.WriteOutput(seed, time.Now())
	}
	c.tr = tr
	c.mode = mode
	c.path = path
	c.startedAt = startedAt
	return nil
}

func (c *traceController) close() error {
	if c.tr == nil {
		c.mode = traceModeNone
		c.path = ""
		c.startedAt = time.Time{}
		return nil
	}
	err := c.tr.Close()
	c.tr = nil
	c.mode = traceModeNone
	c.path = ""
	c.startedAt = time.Time{}
	return err
}

func (c *traceController) recordOutput(b []byte, ts time.Time) {
	if c.tr != nil && (ts.IsZero() || c.startedAt.IsZero() || !ts.Before(c.startedAt)) {
		c.tr.WriteOutput(b, ts)
	}
}

func (c *traceController) recordInput(in inputEvent) {
	if c.tr == nil {
		return
	}
	switch in.kind {
	case inputType:
		c.tr.WriteInput("type", "", in.bytes)
	case inputKey:
		c.tr.WriteInput("key", in.key, in.bytes)
	case inputPaste:
		c.tr.WriteInput("paste", "", in.bytes)
	case inputUnknown:
		c.tr.WriteInput("unknown", "", in.bytes)
	}
}

func (c *traceController) recordResize(cols, rows int) {
	c.cols = cols
	c.rows = rows
	if c.tr != nil {
		c.tr.WriteResize(cols, rows)
	}
}

func nextHotkeyTracePath(outPath string, now time.Time) (string, error) {
	dir := filepath.Dir(outPath)
	base := filepath.Base(outPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "trace"
	}
	prefix := filepath.Join(dir, fmt.Sprintf("%s-trace-%s", stem, now.Format("20060102-150405")))
	for i := 0; ; i++ {
		path := prefix + ".twee"
		if i > 0 {
			path = fmt.Sprintf("%s-%02d.twee", prefix, i+1)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}
