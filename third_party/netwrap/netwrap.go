// Package netwrap runs a command in a private Linux network and records its
// IPv4 traffic. It is an observation tool, not a complete sandbox.
package netwrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	// GVisorVersion is the exact netstack source version used by this module.
	GVisorVersion = "v0.0.0-20260801065709-124e365c3f93"

	defaultMTU             = 1500
	defaultMaxCaptureBytes = 64 << 20
)

// ErrUnsupported means this operating system cannot run netwrap.
var ErrUnsupported = errors.New("netwrap is supported only on Linux")

// Config controls one wrapped command.
type Config struct {
	Command []string
	Dir     string
	Env     []string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Warnings receives netwrap's one-time capture-limit warning. Nil uses
	// Stderr. Warning writes are best-effort: a failed write never
	// interrupts capture or forwarding.
	Warnings io.Writer

	PCAPPath string
	// FlowLogPath optionally writes one JSON object per completed flow. Empty
	// disables flow logging without affecting packet capture.
	FlowLogPath     string
	MaxPCAPBytes    int64
	MTU             int
	DialTimeout     time.Duration
	UDPIdleTimeout  time.Duration
	ShutdownTimeout time.Duration
	// DNSAddress overrides the host DNS server. Empty uses /etc/resolv.conf.
	// This is useful for tests and for hosts with a custom DNS proxy.
	DNSAddress string

	// ExtraFiles are passed to the command as file descriptors 3, 4, and so on.
	// Other file descriptors are closed before the command starts.
	ExtraFiles []*os.File

	// ControllingTTY makes the command a session leader and assigns its stdin
	// terminal as the controlling terminal. Stdin must be an *os.File referring
	// to a terminal when this is true.
	ControllingTTY bool

	PublishTCP []TCPPublication
}

// TCPPublication maps one host TCP listener to the command's private network.
type TCPPublication struct {
	// Listen is a host address such as "127.0.0.1:8080".
	Listen string
	// Guest is a private address such as "10.0.2.100:8080".
	Guest string
}

// Result describes how the command stopped.
type Result struct {
	// ExitCode is the command's exit status. A command terminated by a
	// signal reports the shell convention 128+signal, and Signal is set.
	ExitCode int
	Signal   os.Signal
	Capture  CaptureStats
}

// CaptureStats describes the completed packet capture.
type CaptureStats struct {
	MaxBytes     int64
	BytesWritten int64
	PacketCount  uint64
	Truncated    bool
}

// Process is a started wrapped command. Its methods are safe for concurrent
// use. Wait may be called more than once.
type Process struct {
	process process
}

type process interface {
	pid() int
	wait() (Result, error)
	signal(os.Signal) error
	signalIfLeaderRunning(os.Signal) error
	closeWithGrace(time.Duration) error
}

// Start prepares packet capture and the private network, starts the command,
// and returns only after exec succeeds. Startup failures are returned directly.
func Start(ctx context.Context, cfg Config) (*Process, error) {
	process, err := start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Process{process: process}, nil
}

// PID returns the command's process ID.
func (p *Process) PID() int { return p.process.pid() }

// Wait waits for command, netstack, and recorder completion.
func (p *Process) Wait() (Result, error) { return p.process.wait() }

// Signal forwards sig to the command's managed process group.
func (p *Process) Signal(sig os.Signal) error { return p.process.signal(sig) }

// SignalIfLeaderRunning forwards sig only if the managed leader has not exited.
func (p *Process) SignalIfLeaderRunning(sig os.Signal) error {
	return p.process.signalIfLeaderRunning(sig)
}

// CloseWithGrace sends SIGTERM, escalates to SIGKILL after grace, and waits
// for the command, netstack, and recorder to finish. It is idempotent.
func (p *Process) CloseWithGrace(grace time.Duration) error {
	return p.process.closeWithGrace(grace)
}

func (c Config) normalized() (Config, error) {
	if len(c.Command) == 0 || c.Command[0] == "" {
		return Config{}, errors.New("netwrap: command is required")
	}
	if c.PCAPPath == "" {
		return Config{}, errors.New("netwrap: PCAPPath is required")
	}
	if c.FlowLogPath != "" && c.PCAPPath == c.FlowLogPath {
		return Config{}, errors.New("netwrap: PCAPPath and FlowLogPath must differ")
	}
	if c.MaxPCAPBytes < 0 {
		return Config{}, errors.New("netwrap: MaxPCAPBytes cannot be negative")
	}
	if c.MaxPCAPBytes == 0 {
		c.MaxPCAPBytes = defaultMaxCaptureBytes
	}
	if c.MTU == 0 {
		c.MTU = defaultMTU
	}
	if c.MTU < 576 || c.MTU > 65535 {
		return Config{}, fmt.Errorf("netwrap: MTU %d is outside 576..65535", c.MTU)
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = 30 * time.Second
	}
	if c.UDPIdleTimeout <= 0 {
		c.UDPIdleTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 5 * time.Second
	}
	if c.DNSAddress != "" {
		host, _, err := net.SplitHostPort(c.DNSAddress)
		if err != nil || net.ParseIP(host).To4() == nil {
			return Config{}, fmt.Errorf("netwrap: DNSAddress must be an IPv4 host:port")
		}
	}
	if c.Env == nil {
		c.Env = os.Environ()
	}
	c.PublishTCP = append([]TCPPublication(nil), c.PublishTCP...)
	if c.Stdin == nil {
		c.Stdin = os.Stdin
	}
	if c.ControllingTTY {
		file, ok := c.Stdin.(*os.File)
		if !ok || file == nil {
			return Config{}, errors.New("netwrap: ControllingTTY requires Stdin to be an *os.File")
		}
	}
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if c.Warnings == nil {
		c.Warnings = c.Stderr
	}
	for i, pub := range c.PublishTCP {
		if pub.Listen == "" || pub.Guest == "" {
			return Config{}, fmt.Errorf("netwrap: PublishTCP[%d] needs Listen and Guest", i)
		}
		host, port, err := net.SplitHostPort(pub.Listen)
		if err != nil {
			return Config{}, fmt.Errorf("netwrap: PublishTCP[%d] Listen: %w", i, err)
		}
		if host == "" {
			c.PublishTCP[i].Listen = net.JoinHostPort("127.0.0.1", port)
		}
		guestHost, guestPort, err := net.SplitHostPort(pub.Guest)
		if err != nil || net.ParseIP(guestHost).To4() == nil {
			return Config{}, fmt.Errorf("netwrap: PublishTCP[%d] Guest must be an IPv4 host:port", i)
		}
		if guestHost != "10.0.2.100" {
			return Config{}, fmt.Errorf("netwrap: PublishTCP[%d] Guest host must be 10.0.2.100", i)
		}
		if parsed, err := strconv.ParseUint(guestPort, 10, 16); err != nil || parsed == 0 {
			return Config{}, fmt.Errorf("netwrap: PublishTCP[%d] Guest port must be in 1..65535", i)
		}
	}
	return c, nil
}
