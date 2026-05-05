package codegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/ptyrunner"
	"github.com/paulsmith/research/twee/internal/rpc"
	"github.com/paulsmith/research/twee/internal/vt"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Options configures an interactive codegen run.
type Options struct {
	Command   []string
	Env       map[string]string
	Dir       string
	Cols      int
	Rows      int
	OutPath   string
	TracePath string
	NoWaits   bool

	Stdin  *os.File
	Stdout *os.File
	Stderr io.Writer

	Quiet time.Duration
}

type outputEvent struct {
	bytes []byte
	ts    time.Time
}
type inputBytesEvent struct{ bytes []byte }
type resizeEvent struct {
	cols int
	rows int
}
type fatalEvent struct{ err error }
type ptyDoneEvent struct{}

type warningSummary struct {
	count int
	first string
}

func (w *warningSummary) Add(msg string) {
	if msg == "" {
		return
	}
	w.count++
	if w.first == "" {
		w.first = msg
	}
}

func (w warningSummary) Report(dst io.Writer) {
	if w.count == 0 {
		return
	}
	if w.count == 1 {
		fmt.Fprintf(dst, "\ntwee codegen: %s\n", w.first)
		return
	}
	fmt.Fprintf(dst, "\ntwee codegen: omitted %d unknown input sequences from script; first: %s\n", w.count, w.first)
}

// Run starts the child under a PTY, proxies the user's terminal to it, and
// writes a replayable twee run script when the child exits or the user quits.
func Run(ctx context.Context, opts Options) error {
	if len(opts.Command) == 0 {
		return errors.New("codegen: missing command")
	}
	if opts.OutPath == "" {
		return errors.New("codegen: missing output path")
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Quiet == 0 {
		opts.Quiet = 100 * time.Millisecond
	}
	cols, rows := initialSize(opts)
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stopSignals()

	oldState, err := term.MakeRaw(int(opts.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	fd := int(opts.Stdin.Fd())
	oldFlags, flagsErr := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if flagsErr == nil {
		_ = unix.SetNonblock(fd, true)
	}
	restore := func() {
		if flagsErr == nil {
			_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, oldFlags)
		}
		_ = term.Restore(int(opts.Stdin.Fd()), oldState)
	}
	defer restore()

	env := (&engine.Config{Env: opts.Env}).BuildEnv()
	runner, err := ptyrunner.Start(ctx, ptyrunner.Config{
		Command: opts.Command,
		Env:     env,
		Dir:     opts.Dir,
		Cols:    cols,
		Rows:    rows,
	})
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	model := vt.New(cols, rows)
	rec := &recorder{}
	if err := rec.Resize(cols, rows); err != nil {
		_ = runner.Close()
		return err
	}
	traces := newTraceController(opts, cols, rows, runner.Pid())
	if opts.TracePath != "" {
		if err := traces.startFullSession(opts.TracePath, model.Snapshot()); err != nil {
			_ = runner.Close()
			return fmt.Errorf("trace: %w", err)
		}
	}

	events := make(chan any, 32)
	auxDone := make(chan struct{})
	go readPTY(runner.Master(), events)
	go readStdin(opts.Stdin, events, auxDone)
	go watchResize(opts.Stdout, events, auxDone)

	dec := &Decoder{}
	var warnings warningSummary
	var runErr error
	var stopping bool
	var ptyDone bool
	var outputSinceAction bool
	var waitingForQuiet bool
	var quiet <-chan time.Time
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}

	resetQuiet := func() {
		if opts.NoWaits {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(opts.Quiet)
		quiet = timer.C
	}
	clearQuiet := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		quiet = nil
		waitingForQuiet = false
		outputSinceAction = false
	}
	armAfterAction := func() {
		if opts.NoWaits {
			return
		}
		clearQuiet()
		waitingForQuiet = true
	}
	flushPendingWait := func() {
		if !opts.NoWaits && waitingForQuiet && outputSinceAction {
			if err := rec.WaitStable(); err != nil && runErr == nil {
				runErr = err
			}
		}
		clearQuiet()
	}
	stopChild := func() {
		if stopping {
			return
		}
		stopping = true
		go func() { _ = runner.Close() }()
	}
	handleInput := func(in inputEvent, forward bool) {
		writeInput := func(b []byte) {
			if !forward || len(b) == 0 {
				return
			}
			if _, err := runner.Master().Write(b); err != nil && runErr == nil {
				runErr = err
			}
		}
		switch in.kind {
		case inputControl:
			switch in.control {
			case 'q':
				fmt.Fprint(opts.Stderr, "\r\ntwee codegen: stopping\r\n")
				stopChild()
			case 't':
				if err := traces.toggleHotkey(model.Snapshot()); err != nil && runErr == nil {
					runErr = err
					stopChild()
				}
			case 'd':
				fmt.Fprint(opts.Stderr, "\r\ntwee codegen: detach is reserved for a future named-session backend\r\n")
			default:
				fmt.Fprintf(opts.Stderr, "\r\ntwee codegen: unknown Ctrl+] command %q\r\n", in.control)
			}
		case inputType:
			writeInput(in.bytes)
			if forward {
				traces.recordInput(in)
			}
			if err := rec.Type(in.text); err != nil && runErr == nil {
				runErr = err
			}
			armAfterAction()
		case inputKey:
			writeInput(in.bytes)
			if forward {
				traces.recordInput(in)
			}
			if err := rec.Key(in.key); err != nil && runErr == nil {
				runErr = err
			}
			armAfterAction()
		case inputPaste:
			writeInput(in.bytes)
			if forward {
				traces.recordInput(in)
			}
			if err := rec.Paste(in.text); err != nil && runErr == nil {
				runErr = err
			}
			armAfterAction()
		case inputUnknown:
			writeInput(in.bytes)
			if forward {
				traces.recordInput(in)
			}
			warnings.Add(in.warning)
		}
	}

	for !ptyDone {
		select {
		case ev := <-events:
			switch ev := ev.(type) {
			case outputEvent:
				_, _ = opts.Stdout.Write(ev.bytes)
				_ = model.Feed(ev.bytes)
				traces.recordOutput(ev.bytes, ev.ts)
				if waitingForQuiet {
					outputSinceAction = true
					resetQuiet()
				}
			case inputBytesEvent:
				for _, in := range dec.Decode(ev.bytes) {
					handleInput(in, true)
				}
			case resizeEvent:
				if ev.cols <= 0 || ev.rows <= 0 {
					continue
				}
				cols, rows = ev.cols, ev.rows
				if err := runner.Resize(cols, rows); err != nil && runErr == nil {
					runErr = err
				}
				_ = model.Resize(cols, rows)
				if err := rec.Resize(cols, rows); err != nil && runErr == nil {
					runErr = err
				}
				traces.recordResize(cols, rows)
				armAfterAction()
			case fatalEvent:
				if runErr == nil {
					runErr = ev.err
				}
				stopChild()
			case ptyDoneEvent:
				ptyDone = true
			}
		case <-quiet:
			if waitingForQuiet && outputSinceAction {
				if err := rec.WaitStable(); err != nil && runErr == nil {
					runErr = err
				}
			}
			clearQuiet()
		case <-runner.ExitedCh():
			stopping = true
		case <-ctx.Done():
			runErr = ctx.Err()
			stopChild()
		}
	}

	close(auxDone)
	for _, in := range dec.Flush() {
		handleInput(in, false)
	}
	flushPendingWait()
	traceErr := traces.close()
	restore()
	warnings.Report(opts.Stderr)

	writeErr := writeScript(opts.OutPath, rec.Requests())
	if runErr != nil || traceErr != nil {
		return errors.Join(runErr, traceErr)
	}
	return writeErr
}

func initialSize(opts Options) (int, int) {
	if opts.Cols > 0 && opts.Rows > 0 {
		return opts.Cols, opts.Rows
	}
	cols, rows := opts.Cols, opts.Rows
	if opts.Stdout != nil {
		if ws, err := pty.GetsizeFull(opts.Stdout); err == nil {
			if cols <= 0 {
				cols = int(ws.Cols)
			}
			if rows <= 0 {
				rows = int(ws.Rows)
			}
		}
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return cols, rows
}

func readPTY(r io.ReadWriter, events chan<- any) {
	defer func() { events <- ptyDoneEvent{} }()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			events <- outputEvent{bytes: append([]byte(nil), buf[:n]...), ts: time.Now()}
		}
		if err != nil {
			if !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EIO) && err != io.EOF {
				events <- fatalEvent{err: err}
			}
			return
		}
	}
}

func readStdin(r *os.File, events chan<- any, done <-chan struct{}) {
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case events <- inputBytesEvent{bytes: append([]byte(nil), buf[:n]...)}:
			case <-done:
				return
			}
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				select {
				case <-time.After(10 * time.Millisecond):
					continue
				case <-done:
					return
				}
			}
			return
		}
	}
}

func watchResize(tty *os.File, events chan<- any, done <-chan struct{}) {
	if tty == nil {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	for {
		select {
		case <-done:
			return
		case <-ch:
		}
		ws, err := pty.GetsizeFull(tty)
		if err != nil {
			continue
		}
		select {
		case events <- resizeEvent{cols: int(ws.Cols), rows: int(ws.Rows)}:
		case <-done:
			return
		}
	}
}

func writeScript(path string, ops []rpc.Request) error {
	b, err := json.MarshalIndent(ops, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
