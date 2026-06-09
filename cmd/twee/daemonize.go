package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
)

// readyMessage is what the child writes to the parent over the pipe.
type readyMessage struct {
	Name         string          `json:"name"`
	Socket       string          `json:"socket"`
	PID          int             `json:"pid"`
	Trace        string          `json:"trace,omitempty"`
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

	readyW := os.NewFile(uintptr(readyFD), "ready-pipe")
	lockFile := os.NewFile(uintptr(lockFD), "lock-file")
	_ = lockFile // hold open to keep flock alive

	sock, err := socketPath(name)
	if err != nil {
		failDaemonStartup(readyW, name, err)
	}
	_ = os.Remove(sock) // stale socket; lock confirmed no live owner

	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  cmdv,
		Env:  envOverrides,
		Dir:  dir,
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		failDaemonStartup(readyW, name, fmt.Errorf("engine.Start: %w", err))
	}

	tracePath := os.Getenv(envDaemonTrace)
	if tracePath != "" {
		// Reuse the trace_start handler so `start --trace` and the trace
		// verb produce identical bundles (initial screenshot included).
		resp, err := dispatchRunControl(te, rpc.OpTraceStart, rpc.TraceStartArgs{Out: tracePath})
		if err == nil && !resp.OK {
			err = fmt.Errorf("%s", resp.Error.Message)
		}
		if err != nil {
			_ = te.Close()
			failDaemonStartup(readyW, name, fmt.Errorf("trace start: %w", err))
		}
	}

	l, err := listenUnixSocket(sock)
	if err != nil {
		_ = te.Close()
		failDaemonStartup(readyW, name, fmt.Errorf("listen %s: %w", sock, err))
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(sock)
		_ = te.Close()
		failDaemonStartup(readyW, name, fmt.Errorf("chmod socket: %w", err))
	}

	select {
	case <-te.ExitedCh():
		// Finalize first so a requested trace bundle survives even a
		// child that died inside the observation window.
		_ = daemon.FinalizeArtifacts(te)
		code := te.ExitCode()
		details, _ := json.Marshal(quickExitDetails{
			Name:          name,
			ChildArgv:     append([]string(nil), cmdv...),
			ExitCode:      &code,
			SocketCreated: true,
			TracePath:     te.FinalizedTracePath(),
		})
		_ = l.Close()
		_ = te.Close()
		_ = os.Remove(sock)
		removeLockFile(name)
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
	_ = os.Remove(sock)
	removeLockFile(name)
	os.Exit(0)
}

// failDaemonStartup reports a startup error to the parent, removes the
// session lock (still held via the inherited fd), and exits.
func failDaemonStartup(readyW *os.File, name string, err error) {
	removeLockFile(name)
	writeReadyErr(readyW, name, err)
	os.Exit(1)
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

// daemonize re-execs into daemon mode with the given config, holding
// the named lock file. Returns the ready message read back from the
// child.
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
				return nil, fmt.Errorf("daemon %q already running", name)
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

func daemonize(name, dir string, cmd []string, cols, rows int, envOverrides map[string]string, tracePath string) (readyMessage, error) {
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
