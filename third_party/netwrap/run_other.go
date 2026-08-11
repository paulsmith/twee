//go:build !linux

package netwrap

import (
	"context"
	"os"
	"time"
)

type unsupportedProcess struct{}

func (*unsupportedProcess) pid() int                              { return 0 }
func (*unsupportedProcess) wait() (Result, error)                 { return Result{}, ErrUnsupported }
func (*unsupportedProcess) signal(os.Signal) error                { return ErrUnsupported }
func (*unsupportedProcess) signalIfLeaderRunning(os.Signal) error { return ErrUnsupported }
func (*unsupportedProcess) closeWithGrace(time.Duration) error    { return ErrUnsupported }

// Preflight reports whether this host can provide the required isolation.
func Preflight() error {
	return ErrUnsupported
}

// Run fails closed on non-Linux hosts. It never starts the command.
func Run(context.Context, Config) (Result, error) {
	return Result{}, ErrUnsupported
}

func start(context.Context, Config) (process, error) { return nil, ErrUnsupported }
