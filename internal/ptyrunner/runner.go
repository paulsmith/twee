// Package ptyrunner spawns a process under a PTY.
package ptyrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Config configures a Runner.
type Config struct {
	Command []string
	Env     []string // full env, overrides parent
	Dir     string
	Cols    int
	Rows    int
}

// Runner owns the child process and the PTY master.
type Runner struct {
	cfg Config
	cmd *exec.Cmd

	master *os.File
	exited chan struct{}
	reaped chan struct{}
	exit   exitInfo

	group     *processGroup
	closeOnce sync.Once
	closeErr  error
}

type exitInfo struct {
	err    error
	code   int
	signal syscall.Signal
}

func exitInfoFromProcessState(err error, state *os.ProcessState) exitInfo {
	info := exitInfo{err: err}
	if state == nil {
		return info
	}
	info.code = state.ExitCode()
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		info.signal = ws.Signal()
	}
	return info
}

// Start spawns the process. The PTY master is returned via Master().
func Start(ctx context.Context, cfg Config) (*Runner, error) {
	if len(cfg.Command) == 0 {
		return nil, errors.New("ptyrunner: empty command")
	}
	if cfg.Cols <= 0 {
		cfg.Cols = 80
	}
	if cfg.Rows <= 0 {
		cfg.Rows = 24
	}
	group := newProcessGroup()
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Cancel = func() error {
		return group.signal(syscall.SIGKILL)
	}
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	if cfg.Dir != "" {
		cmd.Dir = cfg.Dir
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cfg.Cols),
		Rows: uint16(cfg.Rows),
	})
	if err != nil {
		group.startFailed()
		return nil, err
	}
	r := &Runner{
		cfg:    cfg,
		cmd:    cmd,
		master: master,
		exited: make(chan struct{}),
		reaped: make(chan struct{}),
		group:  group,
	}
	group.started(cmd.Process.Pid)
	go r.wait()
	return r, nil
}

func (r *Runner) wait() {
	r.group.wait(r.cmd, func(info exitInfo) {
		r.exit = info
		close(r.exited)
	})
	close(r.reaped)
}

// Pid returns the child process ID, or 0 if the process has not started.
func (r *Runner) Pid() int {
	if r.cmd.Process != nil {
		return r.cmd.Process.Pid
	}
	return 0
}

// Master returns the PTY master fd. Reads on it produce app output;
// writes deliver input to the app.
func (r *Runner) Master() io.ReadWriter { return r.master }

// Resize updates the PTY winsize and sends SIGWINCH to the child.
func (r *Runner) Resize(cols, rows int) error {
	if err := pty.Setsize(r.master, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		return err
	}
	if r.cmd.Process != nil {
		if err := r.group.signal(syscall.SIGWINCH); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

// Signal forwards a signal to the child. Returns an error if the
// process has not been started.
func (r *Runner) Signal(sig os.Signal) error {
	if r.cmd == nil || r.cmd.Process == nil {
		return errors.New("ptyrunner: child not started")
	}
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("ptyrunner: unsupported signal %v", sig)
	}
	return r.group.signal(sysSig)
}

func killProcessGroup(pgid int, sysSig syscall.Signal) error {
	if err := syscall.Kill(-pgid, sysSig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

// ExitedCh closes when the session leader exits. On Linux, the leader remains
// waitable until Close so its PID continues to identify the PTY process group.
func (r *Runner) ExitedCh() <-chan struct{} { return r.exited }

// ExitCode is valid after ExitedCh fires.
func (r *Runner) ExitCode() int { return r.exit.code }

// ExitSignal reports the signal that terminated the child, as its
// conventional name (e.g. "SIGTERM"), and true — or ("", false) if the
// child instead exited via a normal exit code. Valid after ExitedCh
// fires.
func (r *Runner) ExitSignal() (string, bool) {
	if r.exit.signal == 0 {
		return "", false
	}
	return unix.SignalName(r.exit.signal), true
}

// DefaultGrace is the SIGTERM-to-SIGKILL escalation window used by
// Close. Exported so callers (engine.Term) can name the same default
// explicitly instead of duplicating the literal.
const DefaultGrace = 250 * time.Millisecond

// Close terminates the child gracefully (SIGTERM, then SIGKILL after
// DefaultGrace) and closes the PTY master. Safe to call multiple times.
func (r *Runner) Close() error {
	return r.CloseWithGrace(DefaultGrace)
}

// CloseWithGrace terminates the child and closes the PTY master, like
// Close, but lets the caller override the SIGTERM-to-SIGKILL escalation
// window. grace <= 0 means "SIGKILL immediately": SIGTERM is still sent
// first (harmless if the process dies from it instead), but there is no
// wait before escalating. Safe to call multiple times.
func (r *Runner) CloseWithGrace(grace time.Duration) error {
	r.closeOnce.Do(func() {
		_ = r.group.signal(syscall.SIGTERM)
		if !r.group.waitDone(grace) {
			_ = r.group.signal(syscall.SIGKILL)
		}
		r.group.release()
		select {
		case <-r.reaped:
		case <-time.After(2 * time.Second):
		}
		r.closeErr = r.master.Close()
	})
	return r.closeErr
}
