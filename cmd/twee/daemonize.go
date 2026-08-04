package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/paulsmith/twee/internal/daemon"
	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

const (
	envDaemonMode    = "TWEE_DAEMON_MODE"
	envDaemonName    = "TWEE_DAEMON_NAME"
	envDaemonReadyFd = "TWEE_DAEMON_READY_FD"
	envDaemonLockFd  = "TWEE_DAEMON_LOCK_FD"
	envDaemonCmd     = "TWEE_DAEMON_CMD"
	envDaemonCols    = "TWEE_DAEMON_COLS"
	envDaemonRows    = "TWEE_DAEMON_ROWS"
	envDaemonDir     = "TWEE_DAEMON_DIR"
	envDaemonEnv     = "TWEE_DAEMON_ENV"
	envDaemonTrace   = "TWEE_DAEMON_TRACE"
	envDaemonNetwork = "TWEE_DAEMON_NETWORK"
	envDaemonPublish = "TWEE_DAEMON_PUBLISH_TCP"
)

// readyMessage is what the child writes to the parent over the pipe.
type readyMessage struct {
	Name         string          `json:"name"`
	Socket       string          `json:"socket"`
	PID          int             `json:"pid"`
	Trace        string          `json:"trace,omitempty"`
	Replaced     bool            `json:"replaced,omitempty"`
	Error        string          `json:"error,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorDetails json.RawMessage `json:"error_details,omitempty"`
}

type quickExitDetails struct {
	Name          string   `json:"name"`
	ChildArgv     []string `json:"child_argv"`
	ExitCode      *int     `json:"exit_code"`
	SocketCreated bool     `json:"socket_created"`
	TracePath     string   `json:"trace_path,omitempty"`
	ArtifactError string   `json:"artifact_error,omitempty"`
}

// inDaemonModeReal returns true when this process was invoked as a daemon child.
// Renamed from inDaemonMode (which is now a thin wrapper in main.go for M1
// compatibility; replaced by this when building the M2 binary).
func inDaemonModeReal() bool {
	return os.Getenv(envDaemonMode) == "1"
}

// runDaemonChildReal is the entry point taken in daemon mode.
func runDaemonChildReal() {
	name := os.Getenv(envDaemonName)
	readyFD, _ := strconv.Atoi(os.Getenv(envDaemonReadyFd))
	lockFD, _ := strconv.Atoi(os.Getenv(envDaemonLockFd))
	cols, _ := strconv.Atoi(os.Getenv(envDaemonCols))
	rows, _ := strconv.Atoi(os.Getenv(envDaemonRows))

	var cmdv []string
	_ = json.Unmarshal([]byte(os.Getenv(envDaemonCmd)), &cmdv)
	var envOverrides map[string]string
	_ = json.Unmarshal([]byte(os.Getenv(envDaemonEnv)), &envOverrides)
	dir := os.Getenv(envDaemonDir)
	networkCapture, _ := strconv.ParseBool(os.Getenv(envDaemonNetwork))
	var publishTCP []engine.TCPPublication
	_ = json.Unmarshal([]byte(os.Getenv(envDaemonPublish)), &publishTCP)
	tracePath := os.Getenv(envDaemonTrace)

	readyW := os.NewFile(uintptr(readyFD), "ready-pipe")
	lockFile := os.NewFile(uintptr(lockFD), "lock-file")
	// The lock file was written with the *launcher's* PID by
	// daemonize (before this child even existed); overwrite it with our
	// own now that we're the process actually holding the flock for the
	// rest of the session's life. Nothing parses this at a byte level
	// today except best-effort tooling (readLockPID, for "start
	// --force"'s wait-for-old-daemon-to-exit step) — but a lock file
	// naming the wrong PID is a footgun for any future reader, manual or
	// automated.
	_, _ = lockFile.Seek(0, 0)
	_ = lockFile.Truncate(0)
	_, _ = fmt.Fprintf(lockFile, "%d\n", os.Getpid())

	sock, err := socketPath(name)
	if err != nil {
		failDaemonStartup(readyW, lockFile, name, err)
	}
	_ = os.Remove(sock) // stale socket; lock confirmed no live owner

	var wholeSessionTrace *engine.WholeSessionTraceConfig
	if networkCapture {
		wholeSessionTrace = &engine.WholeSessionTraceConfig{
			Path:    tracePath,
			Network: &engine.NetworkCaptureConfig{PublishTCP: publishTCP},
		}
	}
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:               cmdv,
		Env:               envOverrides,
		Dir:               dir,
		Cols:              cols,
		Rows:              rows,
		WholeSessionTrace: wholeSessionTrace,
	})
	if err != nil {
		failDaemonStartup(readyW, lockFile, name, fmt.Errorf("engine.Start: %w", err))
	}

	if tracePath != "" && !networkCapture {
		// Reuse the trace_start handler so `start --trace` and the trace
		// verb produce identical bundles.
		resp, err := dispatchRunControl(te, rpc.OpTraceStart, rpc.TraceStartArgs{Out: tracePath})
		if err == nil && !resp.OK {
			err = fmt.Errorf("%s", resp.Error.Message)
		}
		if err != nil {
			_ = te.Close()
			failDaemonStartup(readyW, lockFile, name, fmt.Errorf("trace start: %w", err))
		}
	}

	l, err := listenUnixSocket(sock)
	if err != nil {
		_ = te.Close()
		failDaemonStartup(readyW, lockFile, name, fmt.Errorf("listen %s: %w", sock, err))
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(sock)
		_ = te.Close()
		failDaemonStartup(readyW, lockFile, name, fmt.Errorf("chmod socket: %w", err))
	}

	select {
	case <-te.ExitedCh():
		// Finalize first so a requested trace bundle survives even a
		// child that died inside the observation window.
		finalizeErr := daemon.FinalizeArtifacts(te)
		code := te.ExitCode()
		details, _ := json.Marshal(quickExitDetails{
			Name:          name,
			ChildArgv:     append([]string(nil), cmdv...),
			ExitCode:      &code,
			SocketCreated: true,
			TracePath:     te.FinalizedTracePath(),
			ArtifactError: errorString(finalizeErr),
		})
		_ = l.Close()
		_ = te.Close()
		_ = os.Remove(sock)
		releaseDaemonLock(lockFile, name)
		writeReadyErrCode(readyW, name, rpc.CodeChildExited, "child exited during startup", details)
		os.Exit(0)
	case <-time.After(100 * time.Millisecond):
		// Send ready handshake.
		msg := readyMessage{Name: name, Socket: sock, PID: os.Getpid(), Trace: te.TracePath()}
		_ = json.NewEncoder(readyW).Encode(msg)
		_ = readyW.Close()
	}

	// Detach stdio.
	if devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0); err == nil {
		_ = syscall.Dup2(int(devNull.Fd()), 0)
		_ = syscall.Dup2(int(devNull.Fd()), 1)
		_ = syscall.Dup2(int(devNull.Fd()), 2)
		_ = devNull.Close()
	}

	srv := daemon.NewServer(te)
	go func() {
		<-te.ExitedCh()
		// Make artifacts durable while the socket still answers, so a
		// trace is never left unwritten once clients can no longer ask
		// for it. wait-exit handlers finalize too; both calls are
		// idempotent and this one covers sessions nobody is waiting on.
		_ = daemon.FinalizeArtifacts(te)
		time.Sleep(100 * time.Millisecond)
		srv.Stop()
		_ = l.Close()
	}()

	_ = srv.Serve(context.Background(), l)
	_ = te.Close()
	if !te.TombstoneSuppressed() {
		writeTombstone(name, buildTombstone(name, te))
	}
	_ = os.Remove(sock)
	releaseDaemonLock(lockFile, name)
	os.Exit(0)
}

// buildTombstone summarizes how te's session ended: an explicit "twee
// stop" (t.StopRequested) or the child exiting on its own, with the
// resulting exit code or terminating signal. Only called from the
// natural end-of-session teardown path — never from the CHILD_EXITED
// quick-exit path above, which start already reports directly to its
// caller and which never leaves a socket behind to need a tombstone.
func buildTombstone(name string, te *engine.Term) tombstone {
	ts := tombstone{
		Name:          name,
		Stopped:       te.StopRequested(),
		StoppedAt:     time.Now(),
		Command:       te.Cmd(),
		TracePath:     te.FinalizedTracePath(),
		ArtifactError: errorString(te.ArtifactError()),
	}
	if sig, ok := te.ExitSignal(); ok {
		ts.Signal = sig
	} else {
		code := te.ExitCode()
		ts.ExitCode = &code
	}
	return ts
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// failDaemonStartup reports a startup error to the parent, removes the
// session lock (still held via the inherited fd), and exits.
func failDaemonStartup(readyW, lockFile *os.File, name string, err error) {
	releaseDaemonLock(lockFile, name)
	writeReadyErr(readyW, name, err)
	os.Exit(1)
}

// releaseDaemonLock unlinks the lock while lockFile still holds its flock,
// then closes the inherited descriptor to release ownership. Passing lockFile
// through every daemon teardown path also keeps it strongly reachable for the
// full daemon lifetime instead of allowing its finalizer to close it after the
// startup PID write.
func releaseDaemonLock(lockFile *os.File, name string) {
	removeLockFile(name)
	_ = lockFile.Close()
}

// removeLockFile unlinks the session's lock file. Callers must still hold
// the flock (or know the owner is gone) so a concurrent start cannot be
// racing them; starts guard against the unlink race by re-checking the
// path after locking (see acquireSessionLock).
func removeLockFile(name string) {
	if lp, err := lockPath(name); err == nil {
		_ = os.Remove(lp)
	}
}

func writeReadyErr(w *os.File, name string, err error) {
	writeReadyErrCode(w, name, "", err.Error(), nil)
}

func writeReadyErrCode(w *os.File, name, code, msg string, details json.RawMessage) {
	_ = json.NewEncoder(w).Encode(readyMessage{Name: name, Error: msg, ErrorCode: code, ErrorDetails: details})
	_ = w.Close()
}

// alreadyRunningError reports a start collision: the session lock is
// held by a live daemon of the same name.
type alreadyRunningError struct{ name string }

func (e *alreadyRunningError) Error() string {
	return fmt.Sprintf("daemon %q already running", e.name)
}

// acquireSessionLock creates and flocks the session lock file. After
// locking it verifies the file at the lock path is still the inode it
// locked — a daemon removing its lock at exit can unlink the path between
// our open and flock — and retries with a fresh open when it is not.
func acquireSessionLock(name string) (*os.File, error) {
	lp, err := lockPath(name)
	if err != nil {
		return nil, err
	}
	for range 5 {
		lf, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open lock: %w", err)
		}
		if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = lf.Close()
			if err == syscall.EWOULDBLOCK {
				return nil, &alreadyRunningError{name: name}
			}
			return nil, fmt.Errorf("flock: %w", err)
		}
		var fdSt, pathSt syscall.Stat_t
		if err := syscall.Fstat(int(lf.Fd()), &fdSt); err != nil {
			_ = lf.Close()
			return nil, fmt.Errorf("fstat lock: %w", err)
		}
		if err := syscall.Stat(lp, &pathSt); err == nil && fdSt.Ino == pathSt.Ino && fdSt.Dev == pathSt.Dev {
			return lf, nil
		}
		_ = lf.Close() // path unlinked or replaced under us; retry
	}
	return nil, fmt.Errorf("lock %s: path kept changing during acquisition", lp)
}

// readLockPID reads the PID daemonize wrote into name's lock file, if
// any. Used by "start --force" to wait for a stopped daemon to fully
// exit — releasing its flock — before trying to acquire the lock
// itself. A missing lock file or unparsable contents just means there's
// nothing to wait for (never started, or already fully torn down); the
// caller treats that the same as "not applicable".
func readLockPID(name string) (int, bool) {
	lp, err := lockPath(name)
	if err != nil {
		return 0, false
	}
	raw, err := os.ReadFile(lp)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// waitForPIDExit polls until pid is no longer alive or timeout elapses.
// Best-effort: on timeout it just returns, and the caller's subsequent
// lock acquisition will surface a real ALREADY_RUNNING if the process
// genuinely never exits, rather than this hanging forever.
func waitForPIDExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// daemonize re-execs into daemon mode with the given config, holding
// the named lock file. Returns the ready message read back from the
// child.
func daemonize(name, dir string, cmd []string, cols, rows int, envOverrides map[string]string, tracePath string, networkCapture bool, publishTCP []engine.TCPPublication) (readyMessage, error) {
	if err := validateName(name); err != nil {
		return readyMessage{}, err
	}
	lf, err := acquireSessionLock(name)
	if err != nil {
		return readyMessage{}, err
	}
	_ = lf.Truncate(0)
	_, _ = lf.Seek(0, 0)
	_, _ = fmt.Fprintf(lf, "%d\n", os.Getpid())
	// A fresh start owns the name now; don't let a previous session's
	// exit info be mistaken for this one's.
	removeTombstone(name)

	pr, pw, err := os.Pipe()
	if err != nil {
		removeLockFile(name)
		_ = lf.Close()
		return readyMessage{}, fmt.Errorf("pipe: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		removeLockFile(name)
		_ = lf.Close()
		_ = pr.Close()
		_ = pw.Close()
		return readyMessage{}, fmt.Errorf("os.Executable: %w", err)
	}

	cmdJSON, _ := json.Marshal(cmd)
	envJSON, _ := json.Marshal(envOverrides)
	publishJSON, _ := json.Marshal(publishTCP)

	child := exec.Command(exe)
	child.Env = append(os.Environ(),
		envDaemonMode+"=1",
		envDaemonName+"="+name,
		envDaemonReadyFd+"=3", // ExtraFiles[0]
		envDaemonLockFd+"=4",  // ExtraFiles[1]
		envDaemonCols+"="+strconv.Itoa(cols),
		envDaemonRows+"="+strconv.Itoa(rows),
		envDaemonCmd+"="+string(cmdJSON),
		envDaemonEnv+"="+string(envJSON),
		envDaemonDir+"="+dir,
		envDaemonTrace+"="+tracePath,
		envDaemonNetwork+"="+strconv.FormatBool(networkCapture),
		envDaemonPublish+"="+string(publishJSON),
	)
	child.ExtraFiles = []*os.File{pw, lf}
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		removeLockFile(name)
		_ = lf.Close()
		_ = pr.Close()
		_ = pw.Close()
		return readyMessage{}, fmt.Errorf("start daemon: %w", err)
	}
	_ = pw.Close()
	_ = lf.Close()
	go func() { _ = child.Wait() }()

	dec := json.NewDecoder(pr)
	var msg readyMessage
	// Add a read deadline by pulling bytes via a goroutine + timer.
	type result struct {
		msg readyMessage
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		var m readyMessage
		err := dec.Decode(&m)
		resCh <- result{m, err}
	}()
	select {
	case r := <-resCh:
		_ = pr.Close()
		if r.err != nil {
			return readyMessage{}, fmt.Errorf("ready: %w", r.err)
		}
		msg = r.msg
	case <-time.After(10 * time.Second):
		_ = pr.Close()
		return readyMessage{}, fmt.Errorf("daemon did not signal ready within 10s")
	}
	if msg.Error != "" {
		return msg, fmt.Errorf("daemon failed to start: %s", msg.Error)
	}
	return msg, nil
}
