//go:build linux

package netwrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/paulsmith/twee/third_party/netwrap/internal/netstack"
	"github.com/paulsmith/twee/third_party/netwrap/internal/record"
	"golang.org/x/sys/unix"
)

// Run starts the command and waits for capture completion.
func Run(ctx context.Context, config Config) (Result, error) {
	process, err := Start(ctx, config)
	if err != nil {
		return Result{}, err
	}
	wait := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := process.Wait()
		wait <- struct {
			result Result
			err    error
		}{result: result, err: err}
	}()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer signal.Stop(signals)
	escalating := false
	for {
		select {
		case outcome := <-wait:
			return outcome.result, outcome.err
		case sig := <-signals:
			_ = process.Signal(sig)
			if !escalating {
				escalating = true
				grace := config.ShutdownTimeout
				if grace <= 0 {
					grace = 5 * time.Second
				}
				time.AfterFunc(grace, func() { _ = process.Signal(syscall.SIGKILL) })
			}
		}
	}
}

type processRequest struct {
	signal        syscall.Signal
	grace         time.Duration
	close         bool
	requireLeader bool
	reply         chan error
}

type leaderExit struct {
	result Result
	err    error
	pinned bool
}

type linuxProcess struct {
	command         *exec.Cmd
	network         *netstack.Runtime
	recorder        *record.Recorder
	tunFile         *os.File
	networkNS       *os.File
	shutdownTimeout time.Duration
	ctx             context.Context

	requests  chan processRequest
	done      chan struct{}
	exiting   chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	result    Result
	err       error
}

func start(ctx context.Context, config Config) (_ process, returnErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if err := Preflight(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recorder, err := record.Open(cfg.PCAPPath, cfg.FlowLogPath, cfg.MaxPCAPBytes)
	if err != nil {
		return nil, fmt.Errorf("netwrap: open recorder: %w", err)
	}
	keepRecorder := false
	defer func() {
		if !keepRecorder {
			returnErr = errors.Join(returnErr, recorder.Close())
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parentSocket, childSocket, err := controlSocketPair()
	if err != nil {
		return nil, err
	}
	defer func() { _ = parentSocket.Close() }()
	defer func() { _ = childSocket.Close() }()

	execStatus, execStatusWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("netwrap: create command execution status pipe: %w", err)
	}
	defer func() { _ = execStatus.Close() }()
	defer func() { _ = execStatusWriter.Close() }()

	command, token, err := setupCommand(cfg, childSocket, execStatusWriter)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("netwrap: clone setup process with user, network, and mount namespaces and UID/GID maps: %w", err)
	}
	_ = childSocket.Close()
	_ = execStatusWriter.Close()
	if err := ctx.Err(); err != nil {
		abortSetupCommand(command)
		return nil, err
	}
	if err := unix.Sendmsg(int(parentSocket.Fd()), []byte(token), nil, nil, 0); err != nil {
		abortSetupCommand(command)
		return nil, fmt.Errorf("netwrap: authenticate setup process: %w", err)
	}
	tunFile, setup, err := receiveTUNContext(ctx, parentSocket)
	if err != nil {
		abortSetupCommand(command)
		return nil, err
	}
	keepTUN := false
	defer func() {
		if !keepTUN {
			returnErr = errors.Join(returnErr, tunFile.Close())
		}
	}()

	dnsAddress := cfg.DNSAddress
	if dnsAddress == "" {
		dnsAddress, err = hostDNSAddress()
		if err != nil {
			abortSetupCommand(command)
			return nil, err
		}
	}
	sink := &recordingSink{recorder: recorder, warnings: cfg.Warnings}
	publications := make([]netstack.Publication, len(cfg.PublishTCP))
	for i, item := range cfg.PublishTCP {
		publications[i] = netstack.Publication{Listen: item.Listen, Guest: item.Guest}
	}
	network, err := netstack.New(int(tunFile.Fd()), netstack.Config{
		MTU:            cfg.MTU,
		DialTimeout:    cfg.DialTimeout,
		UDPIdleTimeout: cfg.UDPIdleTimeout,
		DNSAddress:     dnsAddress,
		Publications:   publications,
	}, sink)
	if err != nil {
		abortSetupCommand(command)
		return nil, fmt.Errorf("netwrap: start gVisor netstack for %s: %w", setup.TunName, err)
	}
	keepNetwork := false
	defer func() {
		if !keepNetwork {
			network.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		abortSetupCommand(command)
		return nil, err
	}
	networkNS, err := openNetworkNamespace(command.Process.Pid)
	if err != nil {
		abortSetupCommand(command)
		return nil, fmt.Errorf("netwrap: identify managed network namespace: %w", err)
	}
	keepNetworkNS := false
	defer func() {
		if !keepNetworkNS {
			returnErr = errors.Join(returnErr, networkNS.Close())
		}
	}()
	if err := unix.Sendmsg(int(parentSocket.Fd()), []byte("ready"), nil, nil, 0); err != nil {
		abortSetupCommand(command)
		return nil, fmt.Errorf("netwrap: release setup process to start command: %w", err)
	}
	_ = parentSocket.Close()
	if err := waitForExecStatus(ctx, execStatus); err != nil {
		abortSetupCommand(command)
		return nil, err
	}
	p := &linuxProcess{
		command: command, network: network, recorder: recorder, tunFile: tunFile,
		networkNS: networkNS, shutdownTimeout: cfg.ShutdownTimeout, ctx: ctx,
		requests: make(chan processRequest), done: make(chan struct{}), exiting: make(chan struct{}),
	}
	keepRecorder = true
	keepNetwork = true
	keepTUN = true
	keepNetworkNS = true
	go p.supervise()
	return p, nil
}

func controlSocketPair() (*os.File, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("netwrap: create private setup socket pair: %w", err)
	}
	return os.NewFile(uintptr(fds[0]), "netwrap-supervisor"), os.NewFile(uintptr(fds[1]), "netwrap-setup"), nil
}

func setupCommand(cfg Config, socket, execStatus *os.File) (*exec.Cmd, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, "", fmt.Errorf("netwrap: find current executable for setup re-exec: %w", err)
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("netwrap: make private setup token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	setup := setupConfig{Command: cfg.Command, Dir: cfg.Dir, Env: cfg.Env, MTU: cfg.MTU, ExtraFiles: len(cfg.ExtraFiles)}
	raw, err := json.Marshal(setup)
	if err != nil {
		return nil, "", fmt.Errorf("netwrap: encode setup config: %w", err)
	}
	command := exec.Command(executable)
	command.Stdin = cfg.Stdin
	command.Stdout = cfg.Stdout
	command.Stderr = cfg.Stderr
	// The helper receives the control socket at fd 3 and the dedicated
	// close-on-exec execution-status pipe at fd 4. User ExtraFiles begin at fd
	// 5 and are moved to fd 3 onward only after readiness.
	command.ExtraFiles = append([]*os.File{socket, execStatus}, cfg.ExtraFiles...)
	command.Env = append(stripHelperEnvironment(cfg.Env),
		helperRoleEnv+"="+helperRoleValue,
		helperTokenEnv+"="+token,
		helperConfigEnv+"="+base64.RawURLEncoding.EncodeToString(raw),
	)
	attrs := &syscall.SysProcAttr{
		Cloneflags:                 uintptr(unix.CLONE_NEWUSER | unix.CLONE_NEWNET | unix.CLONE_NEWNS),
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
		Pdeathsig:                  syscall.SIGKILL,
	}
	if cfg.ControllingTTY {
		attrs.Setsid = true
		attrs.Setctty = true
		attrs.Ctty = 0
	} else {
		attrs.Setpgid = true
	}
	command.SysProcAttr = attrs
	return command, token, nil
}

func stripHelperEnvironment(env []string) []string {
	clean := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, helperRoleEnv+"=") ||
			strings.HasPrefix(item, helperTokenEnv+"=") ||
			strings.HasPrefix(item, helperConfigEnv+"=") {
			continue
		}
		clean = append(clean, item)
	}
	return clean
}

func receiveTUN(socket *os.File) (*os.File, setupMessage, error) {
	timeout := unix.NsecToTimeval((30 * time.Second).Nanoseconds())
	if err := unix.SetsockoptTimeval(int(socket.Fd()), unix.SOL_SOCKET, unix.SO_RCVTIMEO, &timeout); err != nil {
		return nil, setupMessage{}, fmt.Errorf("netwrap: set setup response timeout: %w", err)
	}
	message := make([]byte, 64<<10)
	control := make([]byte, unix.CmsgSpace(4))
	n, controlN, flags, _, err := unix.Recvmsg(int(socket.Fd()), message, control, 0)
	if err != nil {
		return nil, setupMessage{}, fmt.Errorf("netwrap: receive namespace setup result: %w", err)
	}
	if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		return nil, setupMessage{}, errors.New("netwrap: namespace setup response was truncated")
	}
	var setup setupMessage
	if err := json.Unmarshal(message[:n], &setup); err != nil {
		return nil, setupMessage{}, fmt.Errorf("netwrap: decode namespace setup result: %w", err)
	}
	if !setup.OK {
		if setup.Error == "" {
			setup.Error = "setup process stopped without a detailed error"
		}
		return nil, setup, fmt.Errorf("netwrap: namespace setup failed: %s", setup.Error)
	}
	messages, err := unix.ParseSocketControlMessage(control[:controlN])
	if err != nil {
		return nil, setup, fmt.Errorf("netwrap: parse TUN descriptor message: %w", err)
	}
	var received []int
	for _, item := range messages {
		fds, err := unix.ParseUnixRights(&item)
		if err != nil {
			return nil, setup, fmt.Errorf("netwrap: parse TUN descriptor rights: %w", err)
		}
		received = append(received, fds...)
	}
	if len(received) != 1 {
		for _, fd := range received {
			_ = unix.Close(fd)
		}
		return nil, setup, fmt.Errorf("netwrap: setup sent %d TUN descriptors; want 1", len(received))
	}
	unix.CloseOnExec(received[0])
	return os.NewFile(uintptr(received[0]), setup.TunName), setup, nil
}

type tunResult struct {
	file  *os.File
	setup setupMessage
	err   error
}

func receiveTUNContext(ctx context.Context, socket *os.File) (*os.File, setupMessage, error) {
	return receiveTUNContextWith(ctx, socket, receiveTUN)
}

func receiveTUNContextWith(
	ctx context.Context,
	socket *os.File,
	receive func(*os.File) (*os.File, setupMessage, error),
) (*os.File, setupMessage, error) {
	// An unbuffered handoff lets the worker retain ownership until this caller
	// selects the result. If cancellation wins, the worker closes any received
	// descriptor instead of leaving it stranded in a buffered channel.
	result := make(chan tunResult)
	go func() {
		file, setup, err := receive(socket)
		received := tunResult{file: file, setup: setup, err: err}
		select {
		case result <- received:
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case received := <-result:
		if err := ctx.Err(); err != nil {
			if received.file != nil {
				_ = received.file.Close()
			}
			_ = socket.Close()
			return nil, setupMessage{}, err
		}
		return received.file, received.setup, received.err
	case <-ctx.Done():
		_ = socket.Close()
		return nil, setupMessage{}, ctx.Err()
	}
}

func waitForExecStatus(ctx context.Context, status *os.File) error {
	result := make(chan error, 1)
	go func() { result <- receiveExecStatus(status) }()
	select {
	case err := <-result:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	case <-ctx.Done():
		_ = status.Close()
		return ctx.Err()
	}
}

func receiveExecStatus(status *os.File) error {
	const maxStatusMessage = 64 << 10
	message, err := io.ReadAll(io.LimitReader(status, maxStatusMessage+1))
	if err != nil {
		return fmt.Errorf("netwrap: receive command execution status: %w", err)
	}
	if len(message) == 0 {
		return nil
	}
	if len(message) > maxStatusMessage {
		return errors.New("netwrap: command execution status was too large")
	}
	var setup setupMessage
	if err := json.Unmarshal(message, &setup); err != nil {
		return fmt.Errorf("netwrap: decode command execution status: %w", err)
	}
	if setup.Error == "" {
		return errors.New("netwrap: command setup failed without a detailed error")
	}
	return fmt.Errorf("netwrap: command setup failed: %s", setup.Error)
}

func (p *linuxProcess) pid() int { return p.command.Process.Pid }

func (p *linuxProcess) wait() (Result, error) {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.err
}

func (p *linuxProcess) signal(sig os.Signal) error {
	return p.requestSignal(sig, false)
}

func (p *linuxProcess) signalIfLeaderRunning(sig os.Signal) error {
	return p.requestSignal(sig, true)
}

func (p *linuxProcess) requestSignal(sig os.Signal, requireLeader bool) error {
	sysSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("netwrap: unsupported signal %v", sig)
	}
	request := processRequest{signal: sysSig, requireLeader: requireLeader, reply: make(chan error, 1)}
	select {
	case p.requests <- request:
		return <-request.reply
	case <-p.exiting:
		return os.ErrProcessDone
	}
}

func (p *linuxProcess) closeWithGrace(grace time.Duration) error {
	p.closeOnce.Do(func() {
		request := processRequest{close: true, grace: grace, reply: make(chan error, 1)}
		select {
		case p.requests <- request:
			<-request.reply
		case <-p.exiting:
		}
	})
	_, err := p.wait()
	return err
}

func (p *linuxProcess) supervise() {
	wait := make(chan leaderExit, 1)
	go func() { wait <- waitForLeader(p.command) }()

	var fatalErr error
	var timer *time.Timer
	var timerChannel <-chan time.Time
	ctxDone := p.ctx.Done()
	stopping := false
	cleanupGrace := p.shutdownTimeout
	var cleanupDeadline time.Time
	startStopping := func(grace time.Duration, err error) {
		if err != nil && fatalErr == nil {
			fatalErr = err
		}
		if stopping {
			deadline := time.Now().Add(grace)
			if grace <= 0 || cleanupDeadline.IsZero() || deadline.Before(cleanupDeadline) {
				cleanupDeadline = deadline
				cleanupGrace = grace
				if timer != nil {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				}
				if grace <= 0 {
					timerChannel = nil
					killProcessGroup(p.pid(), syscall.SIGKILL)
					fatalErr = errors.Join(fatalErr, signalNetworkNamespaceProcesses(p.networkNS, p.pid(), syscall.SIGKILL))
				} else {
					if timer == nil {
						timer = time.NewTimer(grace)
					} else {
						timer.Reset(grace)
					}
					timerChannel = timer.C
				}
			}
			return
		}
		stopping = true
		cleanupGrace = grace
		cleanupDeadline = time.Now().Add(grace)
		killProcessGroup(p.pid(), syscall.SIGTERM)
		fatalErr = errors.Join(fatalErr, signalNetworkNamespaceProcesses(p.networkNS, p.pid(), syscall.SIGTERM))
		if grace <= 0 {
			killProcessGroup(p.pid(), syscall.SIGKILL)
			return
		}
		timer = time.NewTimer(grace)
		timerChannel = timer.C
	}
	var result Result
	for {
		select {
		case leader := <-wait:
			if timer != nil {
				timer.Stop()
			}
			// A command that daemonizes is not supported. Stop any process that
			// remains in the managed process group after the leader exits.
			close(p.exiting)
			if stopping && !cleanupDeadline.IsZero() {
				cleanupGrace = max(time.Until(cleanupDeadline), 0)
			}
			if cleanupDeadline.IsZero() {
				cleanupDeadline = time.Now().Add(cleanupGrace)
				killProcessGroup(p.pid(), syscall.SIGTERM)
				fatalErr = errors.Join(fatalErr, signalNetworkNamespaceProcesses(p.networkNS, p.pid(), syscall.SIGTERM))
			}
			var cleanupErr error
			if leader.pinned {
				cleanupErr = finishPinnedProcessGroup(p.pid(), cleanupGrace)
			} else {
				cleanupErr = finishProcessGroup(p.pid(), cleanupGrace)
			}
			result = leader.result
			namespaceGrace := cleanupGrace
			if !cleanupDeadline.IsZero() {
				namespaceGrace = max(time.Until(cleanupDeadline), 0)
			}
			namespaceErr := finishNetworkNamespaceProcesses(p.networkNS, p.pid(), namespaceGrace)
			if leader.pinned {
				_ = p.command.Wait()
			}
			fatalErr = errors.Join(fatalErr, leader.err, cleanupErr, namespaceErr)
			goto finished
		case request := <-p.requests:
			if request.close {
				startStopping(request.grace, nil)
				request.reply <- nil
				continue
			}
			if request.requireLeader {
				exited, err := leaderExited(p.pid())
				if err != nil {
					request.reply <- err
					continue
				}
				if exited {
					request.reply <- os.ErrProcessDone
					continue
				}
			}
			request.reply <- signalProcessGroup(p.pid(), request.signal)
		case <-ctxDone:
			// A cancelled context's Done channel remains readable forever. Disable
			// this case after the first signal so it cannot busy-loop or repeatedly
			// signal the process group.
			ctxDone = nil
			startStopping(p.shutdownTimeout, p.ctx.Err())
		case err := <-p.network.Errors():
			startStopping(p.shutdownTimeout, err)
		case <-timerChannel:
			killProcessGroup(p.pid(), syscall.SIGKILL)
			fatalErr = errors.Join(fatalErr, signalNetworkNamespaceProcesses(p.networkNS, p.pid(), syscall.SIGKILL))
			timerChannel = nil
		}
	}

finished:
	// Runtime.Close waits for every active proxy and packet callback before the
	// recorder is closed. Therefore a completed Wait is a durable capture
	// boundary: no goroutine can still append to either output.
	p.network.Close()
	// The loop above was the only reader of the 1-buffered error channel. A
	// fatal recording error that raced the leader's exit or arrived during
	// the final proxy drain would otherwise be silently dropped.
	fatalErr = errors.Join(fatalErr, drainNetworkError(p.network.Errors()))
	tunErr := p.tunFile.Close()
	namespaceErr := p.networkNS.Close()
	recorderErr := p.recorder.Close()
	stats := p.recorder.Stats()
	result.Capture = CaptureStats{
		MaxBytes: stats.MaxBytes, BytesWritten: stats.BytesWritten,
		PacketCount: stats.PacketCount, Truncated: stats.Truncated,
	}
	p.mu.Lock()
	p.result = result
	p.err = errors.Join(fatalErr, tunErr, namespaceErr, recorderErr)
	p.mu.Unlock()
	close(p.done)
}

// drainNetworkError reads a fatal network error without blocking, for the
// window after the supervise loop stops selecting on the error channel.
func drainNetworkError(errs <-chan error) error {
	select {
	case err := <-errs:
		return err
	default:
		return nil
	}
}

func leaderExited(pid int) (bool, error) {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, syscall.WEXITED|syscall.WNOHANG|syscall.WNOWAIT, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.ECHILD) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return info.Signo != 0, nil
	}
}

func waitForLeader(command *exec.Cmd) leaderExit {
	pid := command.Process.Pid
	var info unix.Siginfo
	var err error
	for {
		err = unix.Waitid(unix.P_PID, pid, &info, syscall.WEXITED|syscall.WNOWAIT, nil)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err == nil {
		result, resultErr := commandResultFromProc(pid)
		return leaderExit{result: result, err: resultErr, pinned: true}
	}
	waitErr := command.Wait()
	return leaderExit{result: commandResult(waitErr)}
}

func commandResultFromProc(pid int) (Result, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return Result{ExitCode: 125}, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return Result{ExitCode: 125}, errors.New("malformed /proc stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 50 {
		return Result{ExitCode: 125}, errors.New("missing exit_code in /proc stat")
	}
	raw, err := strconv.ParseInt(fields[49], 10, 32)
	if err != nil {
		return Result{ExitCode: 125}, err
	}
	status := syscall.WaitStatus(raw)
	if status.Signaled() {
		sig := status.Signal()
		return Result{ExitCode: 128 + int(sig), Signal: sig}, nil
	}
	return Result{ExitCode: status.ExitStatus()}, nil
}

func commandResult(err error) Result {
	if err == nil {
		return Result{ExitCode: 0}
	}
	exitError, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return Result{ExitCode: 125}
	}
	status, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok {
		return Result{ExitCode: exitError.ExitCode()}
	}
	if status.Signaled() {
		sig := status.Signal()
		return Result{ExitCode: 128 + int(sig), Signal: sig}
	}
	return Result{ExitCode: status.ExitStatus()}
}

func killProcessGroup(pid int, signal syscall.Signal) {
	_ = signalProcessGroup(pid, signal)
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	if pid > 0 {
		if err := syscall.Kill(-pid, signal); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	return os.ErrProcessDone
}

func openNetworkNamespace(pid int) (*os.File, error) {
	return os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
}

func networkNamespaceProcesses(namespace *os.File, excludePID int) ([]int, error) {
	target, err := namespace.Stat()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == excludePID {
			continue
		}
		candidate, err := os.Stat(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err == nil && os.SameFile(target, candidate) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func signalNetworkNamespaceProcesses(namespace *os.File, excludePID int, sig syscall.Signal) error {
	pids, err := networkNamespaceProcesses(namespace, excludePID)
	if err != nil {
		return err
	}
	var signalErr error
	for _, pid := range pids {
		pidfd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			signalErr = errors.Join(signalErr, err)
			continue
		}
		processNS, openErr := openNetworkNamespace(pid)
		targetInfo, targetErr := namespace.Stat()
		var processInfo os.FileInfo
		if openErr == nil {
			processInfo, openErr = processNS.Stat()
			_ = processNS.Close()
		}
		if openErr == nil && targetErr == nil && os.SameFile(targetInfo, processInfo) {
			if err := unix.PidfdSendSignal(pidfd, sig, nil, 0); err != nil && !errors.Is(err, syscall.ESRCH) {
				signalErr = errors.Join(signalErr, err)
			}
		} else if targetErr != nil {
			signalErr = errors.Join(signalErr, targetErr)
		} else if openErr != nil && !errors.Is(openErr, os.ErrNotExist) {
			signalErr = errors.Join(signalErr, openErr)
		}
		_ = unix.Close(pidfd)
	}
	return signalErr
}

func waitForNetworkNamespaceExit(namespace *os.File, excludePID int, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		pids, err := networkNamespaceProcesses(namespace, excludePID)
		if err != nil {
			return false, err
		}
		if len(pids) == 0 {
			return true, nil
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func finishNetworkNamespaceProcesses(namespace *os.File, excludePID int, grace time.Duration) error {
	pids, err := networkNamespaceProcesses(namespace, excludePID)
	if err != nil {
		return fmt.Errorf("netwrap: scan managed network namespace: %w", err)
	}
	if len(pids) == 0 {
		return nil
	}
	if exited, err := waitForNetworkNamespaceExit(namespace, excludePID, grace); err != nil {
		return err
	} else if exited {
		return nil
	}
	killErr := signalNetworkNamespaceUntilExit(namespace, excludePID, syscall.SIGKILL, time.Second)
	if exited, err := waitForNetworkNamespaceExit(namespace, excludePID, 0); err != nil {
		return errors.Join(killErr, err)
	} else if exited {
		return killErr
	}
	return errors.Join(killErr, errors.New("netwrap: managed network namespace has live processes after SIGKILL"))
}

func signalNetworkNamespaceUntilExit(namespace *os.File, excludePID int, sig syscall.Signal, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var signalErr error
	for {
		signalErr = errors.Join(signalErr, signalNetworkNamespaceProcesses(namespace, excludePID, sig))
		pids, err := networkNamespaceProcesses(namespace, excludePID)
		if err != nil {
			return errors.Join(signalErr, err)
		}
		if len(pids) == 0 || !time.Now().Before(deadline) {
			return signalErr
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// abortSetupCommand stops the setup process group and reaps its leader.
func abortSetupCommand(command *exec.Cmd) {
	killProcessGroup(command.Process.Pid, syscall.SIGKILL)
	_ = command.Wait()
}

func stopProcessGroup(pid int, timeout time.Duration) error {
	if !processGroupExists(pid) {
		return nil
	}
	killProcessGroup(pid, syscall.SIGTERM)
	return finishProcessGroup(pid, timeout)
}

func finishProcessGroup(pid int, timeout time.Duration) error {
	if waitForProcessGroupExit(pid, timeout) {
		return nil
	}
	killProcessGroup(pid, syscall.SIGKILL)
	// SIGKILL is asynchronous. Give the kernel a bounded reap window even when
	// the caller requested no graceful TERM interval.
	finalWait := time.Second
	if waitForProcessGroupExit(pid, finalWait) {
		return nil
	}
	return fmt.Errorf("netwrap: managed process group %d remains after SIGKILL", pid)
}

func finishPinnedProcessGroup(pid int, timeout time.Duration) error {
	if waitForLiveDescendantsExit(pid, timeout) {
		return nil
	}
	killProcessGroup(pid, syscall.SIGKILL)
	if waitForLiveDescendantsExit(pid, time.Second) {
		return nil
	}
	return fmt.Errorf("netwrap: managed process group %d has live descendants after SIGKILL", pid)
}

func waitForLiveDescendantsExit(pid int, timeout time.Duration) bool {
	if !processGroupHasLiveDescendants(pid) {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		if !processGroupHasLiveDescendants(pid) {
			return true
		}
	}
	return !processGroupHasLiveDescendants(pid)
}

func processGroupHasLiveDescendants(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		// Failing closed prevents reaping the leader and then signaling a recycled
		// process-group number.
		return true
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == pgid {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 || fields[0] == "Z" {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && group == pgid {
			return true
		}
	}
	return false
}

func waitForProcessGroupExit(pid int, timeout time.Duration) bool {
	if !processGroupExists(pid) {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		if !processGroupExists(pid) {
			return true
		}
	}
	return !processGroupExists(pid)
}

func processGroupExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(-pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func hostDNSAddress() (string, error) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("netwrap: read host DNS config: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1]).To4()
		if ip != nil {
			return net.JoinHostPort(ip.String(), "53"), nil
		}
	}
	return "", errors.New("netwrap: host resolver has no IPv4 nameserver")
}
