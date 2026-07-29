// Package ptyrunner spawns a process under a PTY.
package ptyrunner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
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
	exit   exitInfo
}

type exitInfo struct {
	err  error
	code int
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
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
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
		return nil, err
	}
	r := &Runner{
		cfg:    cfg,
		cmd:    cmd,
		master: master,
		exited: make(chan struct{}),
	}
	go r.wait()
	return r, nil
}

func (r *Runner) wait() {
	err := r.cmd.Wait()
	r.exit.err = err
	if r.cmd.ProcessState != nil {
		r.exit.code = r.cmd.ProcessState.ExitCode()
	}
	close(r.exited)
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
		_ = r.cmd.Process.Signal(syscall.SIGWINCH)
	}
	return nil
}

// Signal forwards a signal to the child. Returns an error if the
// process has not been started.
func (r *Runner) Signal(sig os.Signal) error {
	if r.cmd == nil || r.cmd.Process == nil {
		return errors.New("ptyrunner: child not started")
	}
	return r.cmd.Process.Signal(sig)
}

// ExitedCh closes when the child has been reaped.
func (r *Runner) ExitedCh() <-chan struct{} { return r.exited }

// ExitCode is valid after ExitedCh fires.
func (r *Runner) ExitCode() int { return r.exit.code }

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
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
	}
	exited := false
	if grace > 0 {
		select {
		case <-r.exited:
			exited = true
		case <-time.After(grace):
		}
	}
	if !exited {
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		select {
		case <-r.exited:
		case <-time.After(2 * time.Second):
			// give up; close the PTY anyway
		}
	}
	return r.master.Close()
}
