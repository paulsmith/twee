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
	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/rpc"
	"github.com/paulsmith/twee/internal/vt"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Options configures an interactive wrap run.
type Options struct {
	Command        []string
	Env            map[string]string
	Dir            string
	Cols           int
	Rows           int
	OutPath        string
	TracePath      string
	NetworkCapture bool
	PublishTCP     []engine.TCPPublication
	NoWaits        bool
	NoStatus       bool

	Stdin  *os.File
	Stdout *os.File
	Stderr io.Writer

	Quiet time.Duration
}

// ExitError reports an otherwise successful wrapped command's exit status.
type ExitError struct{ Code int }

func (e *ExitError) Error() string {
	return fmt.Sprintf("wrapped command exited with status %d", e.Code)
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
		fmt.Fprintf(dst, "\ntwee wrap: %s\n", w.first)
		return
	}
	fmt.Fprintf(dst, "\ntwee wrap: omitted %d unknown input sequences from script; first: %s\n", w.count, w.first)
}

// Run starts the child under a PTY, proxies the user's terminal to it, and
// writes a replayable twee run script when the child exits or the user quits.
func Run(ctx context.Context, opts Options) (returnErr error) {
	if len(opts.Command) == 0 {
		return errors.New("wrap: missing command")
	}
	if opts.NetworkCapture && opts.TracePath == "" {
		return errors.New("wrap: network capture requires an immediate trace path")
	}
	if len(opts.PublishTCP) > 0 && !opts.NetworkCapture {
		return errors.New("wrap: TCP publication requires network capture")
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
	statusRows := rows
	compositorEnabled := compositorCapable(opts.NoStatus, os.Getenv("TERM"), term.IsTerminal(int(opts.Stdout.Fd())))
	if compositorEnabled && rows > 1 {
		rows--
	}
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
	network, networkCfg, err := stageNetworkCapture(opts)
	if err != nil {
		return err
	}
	if network != nil {
		defer func() {
			returnErr = joinNetworkCaptureCleanupError(returnErr, network.Cleanup)
		}()
	}
	runner, err := ptyrunner.Start(ctx, ptyrunner.Config{
		Command: opts.Command,
		Env:     env,
		Dir:     opts.Dir,
		Cols:    cols,
		Rows:    rows,
		Network: networkCfg,
	})
	if err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	model := vt.New(cols, rows)
	script := &scriptController{}
	traces := newTraceController(opts, cols, rows, runner.Pid())
	if opts.OutPath != "" {
		if err := script.start(opts.OutPath, cols, rows, false); err != nil {
			_ = runner.Close()
			return fmt.Errorf("script: %w", err)
		}
	}
	if opts.TracePath != "" {
		if err := traces.start(opts.TracePath, model.Snapshot(), runner.InitialTermios()); err != nil {
			_ = runner.Close()
			return fmt.Errorf("trace: %w", err)
		}
	}
	status := statusBar{w: opts.Stdout, enabled: compositorEnabled && statusRows > 1, rows: statusRows, cols: cols, ascii: statusASCII(os.Getenv("TERM")), composited: compositorEnabled}
	var spinner spinnerLifecycle
	defer spinner.close()
	host := hostRenderer{w: opts.Stdout, active: false, hostRows: statusRows, status: status.enabled, preserve: nativeHostStateSaveCapable(os.Getenv("TERM"))}
	if compositorEnabled {
		host.enter()
		host.render(model.Snapshot(), status.line(script, traces), presentationOf(model))
	} else {
		status.draw(script, traces)
	}
	redraw := func() {
		if compositorEnabled {
			host.status = status.enabled
			host.hostRows = statusRows
			host.render(model.Snapshot(), status.line(script, traces), presentationOf(model))
		} else {
			status.draw(script, traces)
		}
	}
	var toastTimer *time.Timer
	var toast <-chan time.Time
	setToast := func(message string) {
		status.toast = message
		if toastTimer == nil {
			toastTimer = time.NewTimer(2 * time.Second)
		} else {
			if !toastTimer.Stop() {
				select {
				case <-toastTimer.C:
				default:
				}
			}
			toastTimer.Reset(2 * time.Second)
		}
		toast = toastTimer.C
	}
	defer func() {
		if toastTimer != nil {
			toastTimer.Stop()
		}
	}()
	spinnerActive := func() <-chan time.Time {
		return spinner.update(script.state == recorderRecording || traces.state == recorderRecording)
	}

	events := make(chan any, 32)
	auxDone := make(chan struct{})
	go readPTY(runner.Master(), events)
	go readStdin(opts.Stdin, events, auxDone)
	go watchResize(opts.Stdout, events, auxDone)

	dec := &Decoder{}
	var mouseFilter statusMouseFilter
	var warnings warningSummary
	var runErr error
	var stopping bool
	var stopRequested bool
	var ptyDone bool
	var outputSinceAction bool
	var hadSessionActivity bool
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
		if !opts.NoWaits && waitingForQuiet && outputSinceAction && script.state == recorderRecording {
			if err := script.rec.WaitStable(); err != nil && runErr == nil {
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
		stopRequested = true
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
				setToast("finalizing")
				// Keep recorders live while the PTY drains so the final child
				// output is represented; finalization happens after EOF below.
				redraw()
				stopChild()
			case 't':
				if traces.wholeSession {
					setToast("network trace runs until exit")
					redraw()
					break
				}
				if traces.state == recorderIdle {
					if err := traces.start("", model.Snapshot(), runner.Termios()); err != nil && runErr == nil {
						runErr = err
					} else if traces.state == recorderRecording {
						setToast("trace started")
						if !compositorEnabled {
							fmt.Fprintf(opts.Stderr, "\r\ntwee wrap: started trace recording: %s\r\n", terminalPath(traces.path))
						}
					}
				} else if traces.state == recorderRecording {
					if err := traces.close(); err != nil && runErr == nil {
						runErr = err
					} else {
						setToast("trace saved")
						if !compositorEnabled {
							fmt.Fprintf(opts.Stderr, "\r\ntwee wrap: stopped trace recording: %s\r\n", terminalPath(traces.path))
						}
					}
				} else {
					setToast("trace already finalized")
				}
				redraw()
			case 's':
				if script.state == recorderIdle {
					if err := script.start("", cols, rows, hadSessionActivity); err != nil && runErr == nil {
						runErr = err
					} else if script.state == recorderRecording && !compositorEnabled {
						fmt.Fprintf(opts.Stderr, "\r\ntwee wrap: started script recording: %s\r\n", terminalPath(script.path))
					}
				} else if script.state == recorderRecording {
					flushPendingWait()
					if err := script.close(); err != nil && runErr == nil {
						runErr = err
					} else if !compositorEnabled {
						fmt.Fprintf(opts.Stderr, "\r\ntwee wrap: saved script: %s\r\n", terminalPath(script.path))
					}
				} else {
					setToast("script already finalized")
				}
				redraw()
			default:
				setToast(fmt.Sprintf("unknown ^] command %q", in.control))
				redraw()
			}
		case inputType:
			writeInput(in.bytes)
			hadSessionActivity = true
			if forward {
				traces.recordInput(in)
			}
			if script.state == recorderRecording {
				if err := script.rec.Type(in.text); err != nil && runErr == nil {
					runErr = err
				}
			}
			armAfterAction()
		case inputKey:
			writeInput(in.bytes)
			hadSessionActivity = true
			if forward {
				traces.recordInput(in)
			}
			if script.state == recorderRecording {
				if err := script.rec.Key(in.key); err != nil && runErr == nil {
					runErr = err
				}
			}
			armAfterAction()
		case inputPaste:
			writeInput(in.bytes)
			hadSessionActivity = true
			if forward {
				traces.recordInput(in)
			}
			if script.state == recorderRecording {
				if err := script.rec.Paste(in.text); err != nil && runErr == nil {
					runErr = err
				}
			}
			armAfterAction()
		case inputUnknown:
			writeInput(in.bytes)
			hadSessionActivity = true
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
				hadSessionActivity = true
				if !compositorEnabled {
					_, _ = opts.Stdout.Write(ev.bytes)
				}
				traces.recordOutput(ev.bytes, ev.ts)
				_ = model.Feed(ev.bytes)
				if replies, ok := model.(vt.PTYReplySource); ok {
					for _, reply := range replies.DrainPTYReplies() {
						if compositorEnabled {
							if _, err := runner.Master().Write(reply); err != nil && runErr == nil {
								runErr = err
							}
							traces.recordTerminalReply(reply)
						}
					}
				}
				redraw()
				if waitingForQuiet {
					outputSinceAction = true
					resetQuiet()
				}
				status.draw(script, traces)
			case inputBytesEvent:
				for _, in := range dec.Decode(mouseFilter.Feed(ev.bytes, rows, status.enabled)) {
					handleInput(in, true)
				}
			case resizeEvent:
				hadSessionActivity = true
				if ev.cols <= 0 || ev.rows <= 0 {
					continue
				}
				cols, statusRows = ev.cols, ev.rows
				status.enabled = compositorEnabled && statusRows > 1
				rows = statusRows
				if status.enabled && rows > 1 {
					rows--
				}
				if err := runner.Resize(cols, rows); err != nil && runErr == nil {
					runErr = err
				}
				_ = model.Resize(cols, rows)
				status.cols, status.rows = cols, statusRows
				host.hostRows, host.status = statusRows, status.enabled
				if script.state == recorderRecording {
					if err := script.rec.Resize(cols, rows); err != nil && runErr == nil {
						runErr = err
					}
				}
				traces.recordResize(cols, rows)
				armAfterAction()
				redraw()
			case fatalEvent:
				if runErr == nil {
					runErr = ev.err
				}
				stopChild()
			case ptyDoneEvent:
				ptyDone = true
			}
		case <-quiet:
			if waitingForQuiet && outputSinceAction && script.state == recorderRecording {
				if err := script.rec.WaitStable(); err != nil && runErr == nil {
					runErr = err
				}
			}
			clearQuiet()
		case <-spinnerActive():
			if script.state == recorderRecording || traces.state == recorderRecording {
				status.phase++
				redraw()
			}
		case <-toast:
			status.toast = ""
			toast = nil
			redraw()
		case <-runner.ExitedCh():
			// The PTY reader will observe EOF/EIO after buffered output drains.
		case <-ctx.Done():
			runErr = ctx.Err()
			stopChild()
		}
	}

	close(auxDone)
	// PTY EOF can race the runner's wait goroutine. Wait for the child status
	// before reporting a natural command failure.
	<-runner.ExitedCh()
	if err := runner.Err(); err != nil && runErr == nil {
		runErr = err
	}
	for _, in := range dec.Decode(mouseFilter.Flush()) {
		handleInput(in, false)
	}
	for _, in := range dec.Flush() {
		handleInput(in, false)
	}
	flushPendingWait()
	traces.recordExit(runner.ExitCode())
	if snapshot, ok := runner.ExitTermios(); ok {
		traces.recordChildPTYTermiosExit(snapshot)
	}
	scriptErr := script.close()
	var networkErr error
	if network != nil {
		networkErr = finalizeNetworkCapture(network, traces, runner)
	}
	traceErr := traces.close()
	closeErr := runner.Close()
	host.close()
	restore()
	if summary := artifactSummary(script, traces); summary != "" {
		fmt.Fprintf(opts.Stderr, "twee wrap: %s\n", summary)
	}
	warnings.Report(opts.Stderr)

	if runErr != nil || networkErr != nil || traceErr != nil || scriptErr != nil || closeErr != nil {
		return errors.Join(runErr, networkErr, traceErr, scriptErr, closeErr)
	}
	if !stopRequested && runner.ExitCode() != 0 {
		return &ExitError{Code: runner.ExitCode()}
	}
	return nil
}

func presentationOf(model vt.Model) vt.Presentation {
	if source, ok := model.(vt.PresentationSource); ok {
		presentation, _ := source.Presentation()
		return presentation
	}
	return vt.Presentation{}
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
			if !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EIO) && !errors.Is(err, io.EOF) {
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
	return os.WriteFile(path, b, 0o600)
}
