//go:build linux

package ptyrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// processGroup keeps the PTY session leader waitable until shutdown. A
// waitable (zombie) leader pins its PID, so the numeric process-group ID cannot
// be recycled while callers may still signal descendants in that group.
type processGroup struct {
	mu          sync.Mutex
	pgid        int
	releaseCh   chan struct{}
	releaseOnce sync.Once
	exitedCh    chan struct{}
	startedCh   chan struct{}
	startedOnce sync.Once
}

var waitid = unix.Waitid

func newProcessGroup() *processGroup {
	return &processGroup{
		releaseCh: make(chan struct{}),
		exitedCh:  make(chan struct{}),
		startedCh: make(chan struct{}),
	}
}

func (g *processGroup) started(pid int) {
	g.mu.Lock()
	g.pgid = pid
	g.mu.Unlock()
	g.startedOnce.Do(func() { close(g.startedCh) })
}

func (g *processGroup) startFailed() { g.startedOnce.Do(func() { close(g.startedCh) }) }

func (g *processGroup) signal(sig syscall.Signal) error {
	<-g.startedCh
	g.mu.Lock()
	defer g.mu.Unlock()
	pgid := g.pgid
	if pgid == 0 {
		return os.ErrProcessDone
	}
	return killProcessGroup(pgid, sig)
}

func (g *processGroup) wait(cmd *exec.Cmd, notify func(exitInfo)) {
	pid := cmd.Process.Pid
	var si unix.Siginfo
	var waitErr error
	for {
		waitErr = waitid(unix.P_PID, pid, &si, syscall.WEXITED|syscall.WNOWAIT, nil)
		if !errors.Is(waitErr, syscall.EINTR) {
			break
		}
	}
	if waitErr != nil {
		// Reaping releases the numeric PID. Disable future group signaling
		// before reaping so a reused PGID can never be targeted.
		g.mu.Lock()
		g.pgid = 0
		g.mu.Unlock()
		err := cmd.Wait()
		close(g.exitedCh)
		notify(exitInfoFromProcessState(err, cmd.ProcessState))
		return
	}
	info, err := waitInfoFromProc(pid)
	if err != nil {
		info.err = fmt.Errorf("read waitable child status: %w", err)
	}
	close(g.exitedCh)
	notify(info)
	<-g.releaseCh
	g.mu.Lock()
	g.pgid = 0
	g.mu.Unlock()
	// Disable signaling before reaping. Cmd.Wait may wait for the context
	// watcher, whose Cancel callback signals through this same processGroup.
	_ = cmd.Wait()
}

func waitInfoFromProc(pid int) (exitInfo, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return exitInfo{}, err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return exitInfo{}, errors.New("malformed /proc stat")
	}
	fields := strings.Fields(string(b[end+1:]))
	// proc_pid_stat(5): exit_code is field 52; fields starts at field 3.
	if len(fields) < 50 {
		return exitInfo{}, errors.New("missing exit_code in /proc stat")
	}
	status64, err := strconv.ParseInt(fields[49], 10, 32)
	if err != nil {
		return exitInfo{}, err
	}
	status := syscall.WaitStatus(status64)
	info := exitInfo{code: status.ExitStatus()}
	if status.Signaled() {
		info.signal = status.Signal()
		info.code = -1
	}
	return info, nil
}

func (g *processGroup) waitDone(grace time.Duration) bool {
	if grace <= 0 {
		return false
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-g.exitedCh:
		g.mu.Lock()
		reapedWithoutIdentity := g.pgid == 0
		g.mu.Unlock()
		// With a pinned leader, request one final group-wide SIGKILL before
		// releasing its numeric identity so no same-group descendant survives.
		return reapedWithoutIdentity
	case <-timer.C:
		return false
	}
}

func (g *processGroup) release() { g.releaseOnce.Do(func() { close(g.releaseCh) }) }
