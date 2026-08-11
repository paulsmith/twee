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
	"github.com/paulsmith/twee/internal/termios"
	"github.com/paulsmith/twee/third_party/netwrap"
	"golang.org/x/sys/unix"
)

// Config configures a Runner.
type Config struct {
	Command []string
	Env     []string // full env, overrides parent
	Dir     string
	Cols    int
	Rows    int
	Network *NetworkConfig
}

type NetworkConfig struct {
	PCAPPath   string
	PublishTCP []netwrap.TCPPublication
}

// Runner owns the child process and the PTY master.
type Runner struct {
	master    *os.File
	backend   runnerBackend
	closeOnce sync.Once
	closeErr  error

	termiosMu    sync.Mutex
	startTermios termios.Snapshot
	exitTermios  *termios.Snapshot
}

type runnerBackend interface {
	pid() int
	exitedCh() <-chan struct{}
	exitInfo() exitInfo
	signal(os.Signal) error
	closeWithGrace(time.Duration) error
	networkCapture() (NetworkCaptureResult, bool)
}

type localBackend struct {
	cmd    *exec.Cmd
	group  *processGroup
	exited chan struct{}
	reaped chan struct{}
	exit   exitInfo
}

type networkBackend struct {
	process *netwrap.Process
	exited  chan struct{}
	exit    exitInfo
	capture NetworkCaptureResult
}

// NetworkCaptureResult describes a completed network capture. It is available
// after ExitedCh closes, at which point the PCAP is closed and stable.
type NetworkCaptureResult struct {
	MaxBytes     int64
	BytesWritten int64
	PacketCount  uint64
	Truncated    bool
}

type exitInfo struct {
	err    error
	code   int
	signal syscall.Signal
}

func exitInfoFromProcessState(err error, state *os.ProcessState) exitInfo {
	if _, ok := errors.AsType[*exec.ExitError](err); ok {
		// A command exit is status, not a runner failure. Backends report
		// setup, supervision, and I/O failures through Err instead.
		err = nil
	}
	info := exitInfo{err: err}
	if state == nil {
		return info
	}
	info.code = state.ExitCode()
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		info.signal = ws.Signal()
		// Match the network backend: signal deaths report the shell's
		// 128+signal convention instead of ProcessState's -1.
		info.code = 128 + int(info.signal)
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
	if cfg.Network != nil {
		return startNetwork(ctx, cfg)
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
	master, slave, err := pty.Open()
	if err != nil {
		group.startFailed()
		return nil, err
	}
	if err := pty.Setsize(master, &pty.Winsize{
		Cols: uint16(cfg.Cols),
		Rows: uint16(cfg.Rows),
	}); err != nil {
		group.startFailed()
		_ = master.Close()
		_ = slave.Close()
		return nil, err
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	startTermios := termios.Capture(slave.Fd())
	if err := cmd.Start(); err != nil {
		group.startFailed()
		_ = master.Close()
		_ = slave.Close()
		return nil, err
	}
	_ = slave.Close()
	backend := &localBackend{cmd: cmd, group: group, exited: make(chan struct{}), reaped: make(chan struct{})}
	r := &Runner{master: master, backend: backend, startTermios: startTermios}
	group.started(cmd.Process.Pid)
	go backend.wait(r.captureExitTermios)
	return r, nil
}

func startNetwork(ctx context.Context, cfg Config) (*Runner, error) {
	if err := netwrap.Preflight(); err != nil {
		return nil, err
	}
	master, slave, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("open PTY: %w", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Cols: uint16(cfg.Cols), Rows: uint16(cfg.Rows)}); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, err
	}
	// Capture-limit warnings arrive after this function's slave copy closes,
	// so netwrap needs its own duplicate to reach the managed terminal.
	warningsFd, err := unix.FcntlInt(slave.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, fmt.Errorf("dup PTY slave for capture warnings: %w", err)
	}
	warnings := os.NewFile(uintptr(warningsFd), slave.Name())
	startTermios := termios.Capture(slave.Fd())
	process, err := netwrap.Start(ctx, netwrap.Config{
		Command: cfg.Command, Env: cfg.Env, Dir: cfg.Dir,
		Stdin: slave, Stdout: slave, Stderr: slave, ControllingTTY: true,
		Warnings:   warnings,
		PCAPPath:   cfg.Network.PCAPPath,
		PublishTCP: cfg.Network.PublishTCP,
	})
	if err != nil {
		_ = master.Close()
		_ = slave.Close()
		_ = warnings.Close()
		return nil, err
	}
	// The command inherited its own descriptor for the slave. Keeping this copy
	// open would prevent the master from observing EOF after command exit.
	_ = slave.Close()
	backend := &networkBackend{process: process, exited: make(chan struct{})}
	r := &Runner{master: master, backend: backend, startTermios: startTermios}
	go func() {
		result, runErr := process.Wait()
		// Wait returns after the recorder closes, so no warning can still be
		// written. Closing the duplicate lets the master observe EOF.
		_ = warnings.Close()
		backend.exit = exitInfo{err: runErr, code: result.ExitCode}
		if sig, ok := result.Signal.(syscall.Signal); ok {
			backend.exit.signal = sig
		}
		backend.capture = NetworkCaptureResult{
			MaxBytes: result.Capture.MaxBytes, BytesWritten: result.Capture.BytesWritten,
			PacketCount: result.Capture.PacketCount, Truncated: result.Capture.Truncated,
		}
		r.captureExitTermios()
		close(backend.exited)
	}()
	return r, nil
}

func (b *localBackend) wait(captureExit func()) {
	b.group.wait(b.cmd, func(info exitInfo) {
		b.exit = info
		captureExit()
		close(b.exited)
	})
	close(b.reaped)
}

// Pid returns the child process ID, or 0 if the process has not started.
func (r *Runner) Pid() int {
	return r.backend.pid()
}

func (b *localBackend) pid() int   { return b.cmd.Process.Pid }
func (b *networkBackend) pid() int { return b.process.PID() }

// Master returns the PTY master fd. Reads on it produce app output;
// writes deliver input to the app.
func (r *Runner) Master() io.ReadWriter { return r.master }

// InitialTermios returns the child PTY state captured immediately before the
// child was started.
func (r *Runner) InitialTermios() termios.Snapshot {
	return termios.CloneSnapshot(r.startTermios)
}

// Termios captures the child PTY's current state.
func (r *Runner) Termios() termios.Snapshot {
	return termios.Capture(r.master.Fd())
}

// ExitTermios returns the state captured when child exit was observed.
func (r *Runner) ExitTermios() (termios.Snapshot, bool) {
	r.termiosMu.Lock()
	defer r.termiosMu.Unlock()
	if r.exitTermios == nil {
		return termios.Snapshot{}, false
	}
	return termios.CloneSnapshot(*r.exitTermios), true
}

func (r *Runner) captureExitTermios() {
	snapshot := termios.Capture(r.master.Fd())
	r.termiosMu.Lock()
	r.exitTermios = &snapshot
	r.termiosMu.Unlock()
}

// Resize updates the PTY winsize and sends SIGWINCH to the child.
func (r *Runner) Resize(cols, rows int) error {
	select {
	case <-r.ExitedCh():
		return os.ErrProcessDone
	default:
	}
	if err := pty.Setsize(r.master, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		return err
	}
	if backend, ok := r.backend.(*localBackend); ok {
		return backend.group.resizeSignal()
	}
	if backend, ok := r.backend.(*networkBackend); ok {
		return backend.process.SignalIfLeaderRunning(syscall.SIGWINCH)
	}
	return r.backend.signal(syscall.SIGWINCH)
}

// Signal forwards a signal to the child. Returns an error if the
// process has not been started.
func (r *Runner) Signal(sig os.Signal) error {
	return r.backend.signal(sig)
}

func (b *localBackend) signal(sig os.Signal) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("ptyrunner: unsupported signal %v", sig)
	}
	return b.group.signal(sysSig)
}

func (b *networkBackend) signal(sig os.Signal) error { return b.process.Signal(sig) }

func killProcessGroup(pgid int, sysSig syscall.Signal) error {
	if err := syscall.Kill(-pgid, sysSig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

// ExitedCh closes when exit information is available. Network-backed runners
// also wait for the netstack and capture recorder to close. On Linux, a local
// runner's leader remains waitable until Close so its PID continues to identify
// the PTY process group.
func (r *Runner) ExitedCh() <-chan struct{} { return r.backend.exitedCh() }

func (b *localBackend) exitedCh() <-chan struct{}   { return b.exited }
func (b *networkBackend) exitedCh() <-chan struct{} { return b.exited }

// ExitCode is valid after ExitedCh fires. A child terminated by a signal
// reports the shell convention 128+signal on both backends; ExitSignal
// names the signal.
func (r *Runner) ExitCode() int { return r.backend.exitInfo().code }

// Err reports a runtime, network, or recorder failure after ExitedCh closes.
// A non-zero command exit is reported through ExitCode rather than Err.
func (r *Runner) Err() error { return r.backend.exitInfo().err }

func (b *localBackend) exitInfo() exitInfo   { return b.exit }
func (b *networkBackend) exitInfo() exitInfo { return b.exit }

// ExitSignal reports the signal that terminated the child, as its
// conventional name (e.g. "SIGTERM"), and true — or ("", false) if the
// child instead exited via a normal exit code. Valid after ExitedCh
// fires.
func (r *Runner) ExitSignal() (string, bool) {
	info := r.backend.exitInfo()
	if info.signal == 0 {
		return "", false
	}
	return unix.SignalName(info.signal), true
}

// NetworkCapture returns capture completion metadata after ExitedCh closes.
func (r *Runner) NetworkCapture() (NetworkCaptureResult, bool) {
	return r.backend.networkCapture()
}

func (b *localBackend) networkCapture() (NetworkCaptureResult, bool) {
	return NetworkCaptureResult{}, false
}

func (b *networkBackend) networkCapture() (NetworkCaptureResult, bool) {
	return b.capture, true
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
		r.closeErr = errors.Join(r.backend.closeWithGrace(grace), r.master.Close())
	})
	return r.closeErr
}

func (b *localBackend) closeWithGrace(grace time.Duration) error {
	_ = b.group.signal(syscall.SIGTERM)
	if !b.group.waitDone(grace) {
		_ = b.group.signal(syscall.SIGKILL)
	}
	b.group.release()
	select {
	case <-b.reaped:
	case <-time.After(2 * time.Second):
		return errors.New("ptyrunner: timed out reaping child")
	}
	return nil
}

func (b *networkBackend) closeWithGrace(grace time.Duration) error {
	err := b.process.CloseWithGrace(grace)
	<-b.exited
	return err
}
