package codegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

type recorderState int

const (
	recorderIdle recorderState = iota
	recorderRecording
	recorderFinalized
	recorderFailed
)

type traceController struct {
	command []string
	env     map[string]string
	cols    int
	rows    int
	pid     int
	stderr  io.Writer

	tr           traceWriter
	state        recorderState
	path         string
	startedAt    time.Time
	reservation  *pathReservation
	exitRecorded bool
}

type traceWriter interface {
	Close() error
	WriteOutput([]byte, time.Time)
	WriteInput(string, string, []byte)
	WriteExit(int)
	WriteResize(int, int)
}

func newTraceController(opts Options, cols, rows, pid int) *traceController {
	return &traceController{
		command: append([]string(nil), opts.Command...),
		env:     opts.Env,
		cols:    cols,
		rows:    rows,
		pid:     pid,
		stderr:  opts.Stderr,
	}
}

func (c *traceController) start(path string, snap vt.Snapshot) error {
	if c.state != recorderIdle {
		return fmt.Errorf("trace already finalized: %s", terminalPath(c.path))
	}
	generated := path == ""
	var reservation *pathReservation
	if generated {
		var err error
		path, reservation, err = reserveRecorderPath("twee-trace", ".twee", time.Now())
		if err != nil {
			c.state = recorderFailed
			return err
		}
	}
	if err := c.open(path, snap); err != nil {
		cleanupReservedPath(path, reservation)
		c.state = recorderFailed
		c.path = path
		return err
	}
	c.reservation = reservation
	return nil
}

func (c *traceController) open(path string, snap vt.Snapshot) error {
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
	c.state = recorderRecording
	c.path = path
	c.startedAt = startedAt
	return nil
}

func (c *traceController) close() error {
	if c.tr == nil {
		return nil
	}
	err := c.tr.Close()
	c.tr = nil
	if err != nil {
		cleanupReservedPath(c.path, c.reservation)
		c.state = recorderFailed
	} else {
		releaseReservation(c.reservation)
		c.state = recorderFinalized
	}
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

func (c *traceController) recordTerminalReply(b []byte) {
	if c.tr != nil && len(b) > 0 {
		c.tr.WriteInput("terminal_reply", "", b)
	}
}

func (c *traceController) recordExit(code int) {
	if c.tr != nil && !c.exitRecorded {
		c.tr.WriteExit(code)
		c.exitRecorded = true
	}
}

func (c *traceController) recordResize(cols, rows int) {
	c.cols = cols
	c.rows = rows
	if c.tr != nil {
		c.tr.WriteResize(cols, rows)
	}
}

func nextRecorderPath(prefix, ext string, now time.Time) (string, error) {
	path, reservation, err := reserveRecorderPath(prefix, ext, now)
	releaseReservation(reservation)
	return path, err
}

func nextRecorderPathInDir(dir, prefix, ext string, now time.Time) (string, error) {
	path, reservation, err := reserveRecorderPathInDir(dir, prefix, ext, now)
	releaseReservation(reservation)
	return path, err
}

type pathReservation struct {
	file *os.File
	info os.FileInfo
}

func reserveRecorderPath(prefix, ext string, now time.Time) (string, *pathReservation, error) {
	return reserveRecorderPathInDir("", prefix, ext, now)
}

func reserveRecorderPathInDir(dir, prefix, ext string, now time.Time) (string, *pathReservation, error) {
	var err error
	if dir == "" {
		dir, err = os.Getwd()
	}
	if err != nil {
		return "", nil, err
	}
	prefix = filepath.Join(dir, fmt.Sprintf("%s-%s", prefix, now.Format("20060102-150405")))
	for i := 0; ; i++ {
		path := prefix + ext
		if i > 0 {
			path = fmt.Sprintf("%s-%02d%s", prefix, i+1, ext)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			info, statErr := f.Stat()
			if statErr != nil {
				_ = f.Close()
				return "", nil, statErr
			}
			return path, &pathReservation{file: f, info: info}, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", nil, err
	}
}

func cleanupReservedPath(path string, reservation *pathReservation) {
	if reservation == nil || reservation.info == nil {
		return
	}
	current, err := os.Stat(path)
	if err == nil && os.SameFile(reservation.info, current) {
		_ = os.Remove(path)
	}
	releaseReservation(reservation)
}

func releaseReservation(reservation *pathReservation) {
	if reservation != nil && reservation.file != nil {
		_ = reservation.file.Close()
		reservation.file = nil
	}
}
