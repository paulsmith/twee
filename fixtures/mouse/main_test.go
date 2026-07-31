package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func TestReportParserHandlesSplitAndCoalescedSGR(t *testing.T) {
	p := newReportParser("sgr")
	if got, err := p.feed([]byte("\x1b[<0;1")); err != nil || len(got) != 0 {
		t.Fatalf("partial feed = %v, %v", got, err)
	}
	got, err := p.feed([]byte(";1M\x1b[<0;1;1m"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"EVENT action=press button=left x=0 y=0 modifiers=",
		"EVENT action=release button=none x=0 y=0 modifiers=",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("reports = %#v, want %#v", got, want)
	}
}

func TestReportParserProtocols(t *testing.T) {
	tests := []struct {
		name   string
		format string
		report string
		want   string
	}{
		{
			name: "x10", format: "x10",
			report: "\x1b[M" + string([]byte{32, 33 + 12, 33 + 4}),
			want:   "EVENT action=press button=left x=12 y=4 modifiers=",
		},
		{
			name: "x10 release", format: "x10",
			report: "\x1b[M" + string([]byte{35, 33 + 12, 33 + 4}),
			want:   "EVENT action=release button=none x=12 y=4 modifiers=",
		},
		{
			name: "utf8 large", format: "utf8",
			report: "\x1b[M \u014d\u01b1",
			want:   "EVENT action=press button=left x=300 y=400 modifiers=",
		},
		{
			name: "sgr hover", format: "sgr",
			report: "\x1b[<35;21;9M",
			want:   "EVENT action=motion button=none x=20 y=8 modifiers=",
		},
		{
			name: "urxvt release", format: "urxvt",
			report: "\x1b[35;3;4M",
			want:   "EVENT action=release button=none x=2 y=3 modifiers=",
		},
		{
			name: "urxvt hover", format: "urxvt",
			report: "\x1b[67;3;4M",
			want:   "EVENT action=motion button=none x=2 y=3 modifiers=",
		},
		{
			name: "modifiers", format: "sgr",
			report: "\x1b[<28;3;4M",
			want:   "EVENT action=press button=left x=2 y=3 modifiers=shift,alt,ctrl",
		},
		{
			name: "wheel down", format: "sgr",
			report: "\x1b[<65;1;1M",
			want:   "EVENT action=press button=wheel_down x=0 y=0 modifiers=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newReportParser(tt.format)
			got, err := p.feed([]byte(tt.report))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("reports = %#v, want %q", got, tt.want)
			}
		})
	}
}

func TestModeSequences(t *testing.T) {
	on, off, err := modeSequences(config{tracking: "any", format: "sgr"})
	if err != nil {
		t.Fatal(err)
	}
	if on != "\x1b[?1003h\x1b[?1006h" || off != "\x1b[?1003l\x1b[?1006l" {
		t.Fatalf("sequences = %q, %q", on, off)
	}
	if _, _, err := modeSequences(config{tracking: "bad", format: "sgr"}); err == nil {
		t.Fatal("unknown tracking accepted")
	}
	if _, _, err := modeSequences(config{tracking: "any", format: "bad"}); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestRunRawSessionAlwaysCleansUp(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name   string
		cfg    config
		input  io.Reader
		output io.Writer
		want   string
	}{
		{
			name:   "normal EOF",
			cfg:    config{format: "sgr"},
			input:  strings.NewReader(""),
			output: &strings.Builder{},
		},
		{
			name:   "READY write error",
			cfg:    config{format: "sgr"},
			input:  strings.NewReader(""),
			output: &failAfterWriter{failAt: 0, err: boom},
			want:   "write READY",
		},
		{
			name:   "parse error",
			cfg:    config{format: "sgr"},
			input:  strings.NewReader("bad"),
			output: &strings.Builder{},
			want:   "parse",
		},
		{
			name:   "read error",
			cfg:    config{format: "sgr"},
			input:  errorReader{err: boom},
			output: &strings.Builder{},
			want:   "read",
		},
		{
			name:   "event write error",
			cfg:    config{format: "sgr", events: 1},
			input:  strings.NewReader("\x1b[<0;1;1M"),
			output: &failAfterWriter{failAt: 1, err: boom},
			want:   "write event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanups := 0
			err := runRawSession(tt.cfg, tt.input, tt.output, "\x1b[?1003h", func() {
				cleanups++
			})
			if cleanups != 1 {
				t.Fatalf("cleanup calls = %d, want 1", cleanups)
			}
			if tt.want == "" {
				if err != nil {
					t.Fatalf("runRawSession: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRunFixtureSignalImmediatelyAfterRawSetup(t *testing.T) {
	log := &eventLog{}
	input := newBlockingReadCloser(log)
	output := &recordingWriter{log: log}
	var signals chan<- os.Signal
	var restoreCalls atomic.Int32

	code := runFixture(config{tracking: "any", format: "sgr"}, fixtureRuntime{
		getFlags: func(fd int) (int, error) {
			if fd != 42 {
				t.Fatalf("get flags fd = %d, want control fd 42", fd)
			}
			log.add("get_flags")
			return 7, nil
		},
		openInput: func(fd int) (readCloser, error) {
			if fd != 42 {
				t.Fatalf("open input fd = %d, want control fd 42", fd)
			}
			log.add("open_input")
			return input, nil
		},
		setFlags: func(fd, flags int) error {
			if fd != 42 || flags != 7 {
				t.Fatalf("restore flags = (%d,%d), want (42,7)", fd, flags)
			}
			log.add("restore_flags")
			return nil
		},
		output:      output,
		errorOutput: &strings.Builder{},
		controlFD:   42,
		isTerminal:  func(int) bool { return true },
		notify: func(c chan<- os.Signal, _ ...os.Signal) {
			log.add("notify")
			signals = c
		},
		stopNotify: func(chan<- os.Signal) {
			log.add("stop_notify")
		},
		makeRaw: func(int) (*term.State, error) {
			log.add("make_raw")
			// This is the vulnerable instant: raw setup has completed, but
			// runFixture has not started the session or its signal worker.
			signals <- os.Interrupt
			return new(term.State), nil
		},
		restore: func(int, *term.State) error {
			restoreCalls.Add(1)
			log.add("restore")
			return nil
		},
	})

	if code != 130 {
		t.Fatalf("exit code = %d, want 130", code)
	}
	if restoreCalls.Load() != 1 {
		t.Fatalf("restore calls = %d, want 1", restoreCalls.Load())
	}
	want := []string{
		"get_flags",
		"open_input",
		"notify",
		"make_raw",
		"input_close",
		"write:\x1b[?1003l\x1b[?1006l",
		"restore_flags",
		"restore",
		"stop_notify",
	}
	if got := log.snapshot(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lifecycle events = %#v, want %#v", got, want)
	}
}

func TestRunFixtureSignalWhileReadyWriteBlocked(t *testing.T) {
	log := &eventLog{}
	input := newBlockingReadCloser(log)
	output := newBlockingFirstWriter(log)
	var signals chan<- os.Signal
	var restoreCalls atomic.Int32
	restored := make(chan struct{})
	result := make(chan int, 1)

	go func() {
		result <- runFixture(config{tracking: "any", format: "sgr"}, fixtureRuntime{
			getFlags: func(fd int) (int, error) {
				if fd != 42 {
					t.Errorf("get flags fd = %d, want control fd 42", fd)
				}
				log.add("get_flags")
				return 7, nil
			},
			openInput: func(fd int) (readCloser, error) {
				if fd != 42 {
					t.Errorf("open input fd = %d, want control fd 42", fd)
				}
				log.add("open_input")
				return input, nil
			},
			setFlags: func(fd, flags int) error {
				if fd != 42 || flags != 7 {
					t.Errorf("restore flags = (%d,%d), want (42,7)", fd, flags)
				}
				log.add("restore_flags")
				return nil
			},
			output:      output,
			errorOutput: &strings.Builder{},
			controlFD:   42,
			isTerminal:  func(int) bool { return true },
			notify: func(c chan<- os.Signal, _ ...os.Signal) {
				log.add("notify")
				signals = c
			},
			stopNotify: func(chan<- os.Signal) {
				log.add("stop_notify")
			},
			makeRaw: func(int) (*term.State, error) {
				log.add("make_raw")
				return new(term.State), nil
			},
			restore: func(int, *term.State) error {
				if restoreCalls.Add(1) == 1 {
					close(restored)
				}
				log.add("restore")
				return nil
			},
		})
	}()

	waitFor(t, output.firstWriteStarted, "enable/READY write to start")
	signals <- syscall.SIGTERM
	waitFor(t, input.closed, "signal worker to interrupt input")

	select {
	case <-restored:
		t.Fatal("terminal restored while session output was still blocked")
	default:
	}

	close(output.releaseFirstWrite)
	select {
	case code := <-result:
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runFixture did not return after releasing output")
	}

	if restoreCalls.Load() != 1 {
		t.Fatalf("restore calls = %d, want 1", restoreCalls.Load())
	}
	got := log.snapshot()
	want := []string{
		"get_flags",
		"open_input",
		"notify",
		"make_raw",
		"write_start:\x1b[?1003h\x1b[?1006hREADY\r\n",
		"input_close",
		"write_end:\x1b[?1003h\x1b[?1006hREADY\r\n",
		"write_start:\x1b[?1003l\x1b[?1006l",
		"write_end:\x1b[?1003l\x1b[?1006l",
		"restore_flags",
		"restore",
		"stop_notify",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lifecycle events = %#v, want %#v", got, want)
	}
}

func TestRunFixtureRestoreFailureIsFatal(t *testing.T) {
	boom := errors.New("restore boom")
	input := &countingReadCloser{Reader: strings.NewReader("")}
	output := &strings.Builder{}
	errorOutput := &strings.Builder{}

	code := runFixture(config{tracking: "any", format: "sgr"}, fixtureRuntime{
		getFlags: func(int) (int, error) { return 7, nil },
		openInput: func(int) (readCloser, error) {
			return input, nil
		},
		setFlags:    func(int, int) error { return nil },
		output:      output,
		errorOutput: errorOutput,
		controlFD:   42,
		isTerminal:  func(int) bool { return true },
		makeRaw:     func(int) (*term.State, error) { return new(term.State), nil },
		restore:     func(int, *term.State) error { return boom },
		notify:      func(chan<- os.Signal, ...os.Signal) {},
		stopNotify:  func(chan<- os.Signal) {},
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if input.closeCalls.Load() != 1 {
		t.Fatalf("input close calls = %d, want 1", input.closeCalls.Load())
	}
	if got := errorOutput.String(); !strings.Contains(got, "restore terminal: restore boom") {
		t.Fatalf("stderr = %q, want surfaced restore failure", got)
	}
	if got, want := output.String(),
		"\x1b[?1003h\x1b[?1006hREADY\r\n\x1b[?1003l\x1b[?1006l"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunFixtureFlagRestoreFailureIsFatal(t *testing.T) {
	boom := errors.New("restore stdin flags: flags boom")
	input := &countingReadCloser{Reader: strings.NewReader("")}
	errorOutput := &strings.Builder{}
	var termRestoreCalls atomic.Int32

	code := runFixture(config{tracking: "any", format: "sgr"}, fixtureRuntime{
		getFlags: func(int) (int, error) { return 7, nil },
		openInput: func(int) (readCloser, error) {
			return input, nil
		},
		setFlags: func(fd, flags int) error {
			if fd != 42 || flags != 7 {
				t.Fatalf("restore flags = (%d,%d), want (42,7)", fd, flags)
			}
			if input.closeCalls.Load() != 1 {
				t.Fatalf(
					"input close calls at flag restore = %d, want 1",
					input.closeCalls.Load(),
				)
			}
			return boom
		},
		output:      &strings.Builder{},
		errorOutput: errorOutput,
		controlFD:   42,
		isTerminal:  func(int) bool { return true },
		makeRaw:     func(int) (*term.State, error) { return new(term.State), nil },
		restore: func(int, *term.State) error {
			termRestoreCalls.Add(1)
			return nil
		},
		notify:     func(chan<- os.Signal, ...os.Signal) {},
		stopNotify: func(chan<- os.Signal) {},
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if termRestoreCalls.Load() != 1 {
		t.Fatalf("terminal restore calls = %d, want 1", termRestoreCalls.Load())
	}
	if got := errorOutput.String(); !strings.Contains(got, "restore stdin flags: flags boom") {
		t.Fatalf("stderr = %q, want surfaced flag restoration failure", got)
	}
}

const mouseFixtureHelperEnv = "TWEE_MOUSE_FIXTURE_PTY_HELPER"

func TestRunFixtureRestoresRealPTYAfterSignal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})

	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatalf("term.GetState before: %v", err)
	}
	beforeFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("F_GETFL before: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMouseFixturePTYHelper$")
	cmd.Env = append(os.Environ(), mouseFixtureHelperEnv+"=1")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	readUntil(t, master, []byte("READY\r\n"))
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal helper: %v", err)
	}

	waitErr := waitProcess(t, cmd)
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("helper wait error = %v, want exit code 1", waitErr)
	}

	after, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatalf("term.GetState after: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("termios not restored after signal:\nbefore: %#v\nafter:  %#v", before, after)
	}
	afterFlags, err := unix.FcntlInt(slave.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatalf("F_GETFL after: %v", err)
	}
	if afterFlags != beforeFlags {
		t.Fatalf(
			"file status flags not restored after signal: before=%#x after=%#x",
			beforeFlags,
			afterFlags,
		)
	}
}

func TestMouseFixturePTYHelper(t *testing.T) {
	if os.Getenv(mouseFixtureHelperEnv) != "1" {
		return
	}
	os.Exit(runFixture(
		config{tracking: "any", format: "sgr"},
		systemFixtureRuntime(),
	))
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type failAfterWriter struct {
	writes int
	failAt int
	err    error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAt {
		return 0, w.err
	}
	w.writes++
	return len(p), nil
}

type countingReadCloser struct {
	io.Reader
	closeCalls atomic.Int32
	closeErr   error
}

func (r *countingReadCloser) Close() error {
	r.closeCalls.Add(1)
	return r.closeErr
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type blockingReadCloser struct {
	log    *eventLog
	closed chan struct{}
	once   sync.Once
}

func newBlockingReadCloser(log *eventLog) *blockingReadCloser {
	return &blockingReadCloser{log: log, closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *blockingReadCloser) Close() error {
	r.once.Do(func() {
		r.log.add("input_close")
		close(r.closed)
	})
	return nil
}

type recordingWriter struct {
	log *eventLog
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.log.add("write:" + string(p))
	return len(p), nil
}

type blockingFirstWriter struct {
	log               *eventLog
	firstWriteStarted chan struct{}
	releaseFirstWrite chan struct{}
	writes            atomic.Int32
}

func newBlockingFirstWriter(log *eventLog) *blockingFirstWriter {
	return &blockingFirstWriter{
		log:               log,
		firstWriteStarted: make(chan struct{}),
		releaseFirstWrite: make(chan struct{}),
	}
}

func (w *blockingFirstWriter) Write(p []byte) (int, error) {
	payload := string(p)
	w.log.add("write_start:" + payload)
	if w.writes.Add(1) == 1 {
		close(w.firstWriteStarted)
		<-w.releaseFirstWrite
	}
	w.log.add("write_end:" + payload)
	return len(p), nil
}

func waitFor(t *testing.T, ch <-chan struct{}, event string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", event)
	}
}

func readUntil(t *testing.T, input *os.File, want []byte) {
	t.Helper()
	if err := input.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set PTY read deadline: %v", err)
	}
	defer func() {
		_ = input.SetReadDeadline(time.Time{})
	}()

	var received []byte
	buf := make([]byte, 256)
	for !bytes.Contains(received, want) {
		n, err := input.Read(buf)
		received = append(received, buf[:n]...)
		if err != nil {
			t.Fatalf("read PTY waiting for %q: %v (received %q)", want, err, received)
		}
	}
}

func waitProcess(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
	}()
	select {
	case err := <-waited:
		return err
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatal("timed out waiting for helper process")
		return nil
	}
}
