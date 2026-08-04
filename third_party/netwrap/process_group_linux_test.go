//go:build linux

package netwrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const abortSetupHelperEnv = "_NETWRAP_ABORT_SETUP_TEST_HELPER"

func TestAbortSetupCommandKillsAndReapsLeader(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestAbortSetupCommandHelper$")
	command.Env = append(os.Environ(), abortSetupHelperEnv+"=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	abortSetupCommand(command)

	if command.ProcessState == nil {
		t.Fatal("setup command leader was not reaped")
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("wait status = %T; want syscall.WaitStatus", command.ProcessState.Sys())
	}
	if !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wait status = %v; want SIGKILL", status)
	}
	if processGroupExists(pid) {
		t.Fatalf("setup process group %d survived abort", pid)
	}
}

func TestAbortSetupCommandHelper(t *testing.T) {
	if os.Getenv(abortSetupHelperEnv) != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestStopProcessGroupKillsTermIgnoringChild(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	readyPath := filepath.Join(dir, "ready")
	command := exec.Command("/bin/sh", "-c", `(trap "" TERM; : > "$2"; exec sleep 1000) & child=$!; while [ ! -e "$2" ]; do :; done; echo "$child" > "$1"; exit 0`, "sh", pidPath, readyPath)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("child PID output %q: %v", output, err)
	}
	if childPID == command.Process.Pid {
		t.Fatalf("child PID %d matches leader PID", childPID)
	}
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })
	if err := stopProcessGroup(command.Process.Pid, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Fatalf("child %d survived process-group cleanup", childPID)
	}
}

func TestRunCleansDescendantThatEscapesProcessGroup(t *testing.T) {
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skipf("setsid unavailable: %v", err)
	}
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "detached.pid")
	pcapPath := filepath.Join(dir, "capture.pcap")
	command := fmt.Sprintf(`%s /bin/sh -c 'trap "" TERM; sleep 1000' & echo $! > "$1"`, setsid)
	result, err := Run(context.Background(), Config{
		Command:         []string{"/bin/sh", "-c", command, "sh", pidPath},
		PCAPPath:        pcapPath,
		ShutdownTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("detached PID %q: %v", raw, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("detached descendant %d survived capture finalization", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReceiveExecStatus(t *testing.T) {
	t.Run("successful exec closes pipe", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		writer.Close()
		defer reader.Close()
		if err := receiveExecStatus(reader); err != nil {
			t.Fatalf("receiveExecStatus() = %v", err)
		}
	})
	t.Run("post readiness failure is an API error", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		message := []byte(`{"error":"exec command: permission denied"}`)
		if _, err := writer.Write(message); err != nil {
			t.Fatal(err)
		}
		writer.Close()
		err = receiveExecStatus(reader)
		if err == nil || err.Error() != "netwrap: command setup failed: exec command: permission denied" {
			t.Fatalf("receiveExecStatus() = %v", err)
		}
	})
}

func TestExecStatusPreservationFailureUsesSetupProtocol(t *testing.T) {
	parentSocket, childSocket, err := controlSocketPair()
	if err != nil {
		t.Fatal(err)
	}
	defer parentSocket.Close()
	defer childSocket.Close()

	execStatusFD, setupErr := sendSetupSuccess(int(childSocket.Fd()), -1, -1, "netwrap0")
	if setupErr == nil || execStatusFD != -1 {
		t.Fatalf("sendSetupSuccess() = fd %d, error %v; want preservation failure", execStatusFD, setupErr)
	}
	sendHelperFailure(int(childSocket.Fd()), setupErr)
	tunFile, _, receiveErr := receiveTUN(parentSocket)
	if tunFile != nil {
		tunFile.Close()
		t.Fatal("receiveTUN returned a TUN file after preservation failure")
	}
	if receiveErr == nil || !strings.Contains(receiveErr.Error(), "preserve command execution status descriptor") {
		t.Fatalf("receiveTUN() error = %v; want setup-protocol preservation error", receiveErr)
	}
}

func TestReceiveTUNContextCancellationClosesConcurrentFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	socket, socketPeer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	defer socketPeer.Close()
	receivedFile, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = receivedFile.Close() })
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		file, _, err := receiveTUNContextWith(ctx, socket, func(*os.File) (*os.File, setupMessage, error) {
			close(started)
			<-release
			return receivedFile, setupMessage{OK: true}, nil
		})
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	<-started
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("receiveTUNContextWith() = %v; want context.Canceled", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		_, err := receivedFile.Stat()
		if errors.Is(err, os.ErrClosed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("concurrently received TUN file remained open: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWaitForExecStatusCancellation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForExecStatus(ctx, reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForExecStatus() = %v; want context.Canceled", err)
	}
	if _, err := writer.WriteString("status must not go to an ExtraFile"); !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("write after status reader close = %v; want EPIPE", err)
	}
}

func TestDrainNetworkError(t *testing.T) {
	errs := make(chan error, 1)
	if err := drainNetworkError(errs); err != nil {
		t.Fatalf("drainNetworkError(empty) = %v; want nil", err)
	}
	want := errors.New("late recording failure")
	errs <- want
	if err := drainNetworkError(errs); !errors.Is(err, want) {
		t.Fatalf("drainNetworkError() = %v; want %v", err, want)
	}
}

func TestRunCanceledBeforeStartupDoesNotCreateOutputs(t *testing.T) {
	dir := t.TempDir()
	pcapPath := filepath.Join(dir, "packets.pcap")
	flowPath := filepath.Join(dir, "flows.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Run(ctx, Config{
		Command:         []string{"/bin/true"},
		PCAPPath:        pcapPath,
		FlowLogPath:     flowPath,
		DNSAddress:      "127.0.0.1:53",
		ShutdownTimeout: time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v; want context.Canceled", err)
	}
	if result != (Result{}) {
		t.Fatalf("Run() result = %+v; want zero Result", result)
	}
	for _, path := range []string{pcapPath, flowPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("output %s exists or could not be checked: %v", path, err)
		}
	}
}

func TestRunReportsPostReadinessExecFailure(t *testing.T) {
	if err := Preflight(); err != nil {
		t.Skipf("netwrap prerequisites unavailable: %v", err)
	}
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "not-an-executable-format")
	if err := os.WriteFile(commandPath, []byte("this is not an ELF binary or script\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	extraFile, err := os.OpenFile(filepath.Join(dir, "user-extra-file"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer extraFile.Close()
	var stderr bytes.Buffer
	result, err := Run(context.Background(), Config{
		Command:     []string{commandPath},
		PCAPPath:    filepath.Join(dir, "packets.pcap"),
		FlowLogPath: filepath.Join(dir, "flows.jsonl"),
		DNSAddress:  "127.0.0.1:53",
		Stderr:      &stderr,
		ExtraFiles:  []*os.File{extraFile},
	})
	if err == nil {
		t.Fatalf("Run() result = %+v, nil error; want execution setup error", result)
	}
	if result.ExitCode == 125 {
		t.Fatalf("Run() result = %+v; post-readiness failure must not be exit 125", result)
	}
	if !strings.Contains(err.Error(), "command setup failed") || !strings.Contains(err.Error(), "exec format error") {
		t.Fatalf("Run() error = %v; want post-readiness exec error", err)
	}
	info, statErr := extraFile.Stat()
	if statErr != nil {
		t.Fatalf("stat user ExtraFile: %v", statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("user ExtraFile received status data: size=%d", info.Size())
	}
}
