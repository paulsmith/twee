//go:build !linux

package ptyrunner

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Non-Linux systems use the portable reap-first behavior. Linux provides the
// stronger PID-reuse guarantee required for descendants that outlive a leader.
type processGroup struct {
	mu          sync.Mutex
	pgid        int
	exited      bool
	exitedCh    chan struct{}
	startedCh   chan struct{}
	startedOnce sync.Once
}

func newProcessGroup() *processGroup {
	return &processGroup{exitedCh: make(chan struct{}), startedCh: make(chan struct{})}
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
	if g.pgid == 0 || g.exited {
		return os.ErrProcessDone
	}
	return killProcessGroup(g.pgid, sig)
}
func (g *processGroup) resizeSignal() error { return g.signal(syscall.SIGWINCH) }
func (g *processGroup) wait(cmd *exec.Cmd, notify func(exitInfo)) {
	err := cmd.Wait()
	g.mu.Lock()
	g.exited = true
	g.pgid = 0
	g.mu.Unlock()
	close(g.exitedCh)
	notify(exitInfoFromProcessState(err, cmd.ProcessState))
}
func (g *processGroup) waitDone(grace time.Duration) bool {
	if grace <= 0 {
		return false
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-g.exitedCh:
		return true
	case <-timer.C:
		return false
	}
}
func (g *processGroup) release() {}
