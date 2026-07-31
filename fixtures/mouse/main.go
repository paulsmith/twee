// Command mouse is a small PTY fixture that enables terminal mouse reporting
// and prints each received report in a stable, protocol-independent form.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type config struct {
	tracking string
	format   string
	events   int
}

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config{}
	flag.StringVar(&cfg.tracking, "tracking", "any", "mouse tracking: x10, normal, button, any")
	flag.StringVar(&cfg.format, "format", "sgr", "mouse format: x10, utf8, sgr, urxvt")
	flag.IntVar(&cfg.events, "events", 0, "exit after this many reports (zero waits forever)")
	flag.Parse()

	return runFixture(cfg, systemFixtureRuntime())
}

func systemFixtureRuntime() fixtureRuntime {
	return fixtureRuntime{
		openInput:   duplicateTerminalInput,
		output:      os.Stdout,
		errorOutput: os.Stderr,
		controlFD:   int(os.Stdin.Fd()),
		getFlags:    terminalFileFlags,
		setFlags:    restoreTerminalFileFlags,
		isTerminal:  term.IsTerminal,
		makeRaw:     term.MakeRaw,
		restore:     term.Restore,
		notify:      signal.Notify,
		stopNotify:  signal.Stop,
	}
}

func duplicateTerminalInput(fd int) (readCloser, error) {
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("duplicate stdin: %w", err)
	}
	unix.CloseOnExec(duplicate)
	if err := unix.SetNonblock(duplicate, true); err != nil {
		_ = unix.Close(duplicate)
		return nil, fmt.Errorf("make duplicate stdin nonblocking: %w", err)
	}
	return os.NewFile(uintptr(duplicate), "mouse fixture stdin"), nil
}

func terminalFileFlags(fd int) (int, error) {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return 0, fmt.Errorf("read stdin flags: %w", err)
	}
	return flags, nil
}

func restoreTerminalFileFlags(fd, flags int) error {
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags); err != nil {
		return fmt.Errorf("restore stdin flags: %w", err)
	}
	return nil
}

type readCloser interface {
	io.Reader
	io.Closer
}

type fixtureRuntime struct {
	openInput   func(int) (readCloser, error)
	output      io.Writer
	errorOutput io.Writer
	controlFD   int
	getFlags    func(int) (int, error)
	setFlags    func(int, int) error
	isTerminal  func(int) bool
	makeRaw     func(int) (*term.State, error)
	restore     func(int, *term.State) error
	notify      func(chan<- os.Signal, ...os.Signal)
	stopNotify  func(chan<- os.Signal)
}

func runFixture(cfg config, runtime fixtureRuntime) (exitCode int) {
	enable, disable, err := modeSequences(cfg)
	if err != nil {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture:", err)
		return 2
	}
	if !runtime.isTerminal(runtime.controlFD) {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture: stdin is not a TTY")
		return 2
	}

	originalFlags, err := runtime.getFlags(runtime.controlFD)
	if err != nil {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture:", err)
		return 1
	}
	rawInput, err := runtime.openInput(runtime.controlFD)
	if err != nil {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture:", err)
		return 1
	}
	input := &onceReadCloser{readCloser: rawInput}

	inputFinalized := false
	var finalizeInputOnce sync.Once
	var finalizeInputErr error
	finalizeInput := func() error {
		finalizeInputOnce.Do(func() {
			inputFinalized = true
			if closeErr := input.Close(); closeErr != nil {
				finalizeInputErr = errors.Join(
					finalizeInputErr,
					fmt.Errorf("close input: %w", closeErr),
				)
			}
			if flagsErr := runtime.setFlags(runtime.controlFD, originalFlags); flagsErr != nil {
				finalizeInputErr = errors.Join(finalizeInputErr, flagsErr)
			}
		})
		return finalizeInputErr
	}
	defer func() {
		if inputFinalized {
			return
		}
		if finalizeErr := finalizeInput(); finalizeErr != nil {
			fmt.Fprintln(runtime.errorOutput, "mouse fixture:", finalizeErr)
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}()

	// Arm notification before MakeRaw so a signal cannot terminate the
	// process in the window after the terminal has changed but before the
	// session's cancellation worker starts.
	signals := make(chan os.Signal, 1)
	runtime.notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer runtime.stopNotify(signals)

	oldState, err := runtime.makeRaw(runtime.controlFD)
	if err != nil {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture: raw mode:", err)
		return 1
	}

	cleanup := func() error {
		var cleanupErr error
		if _, writeErr := io.WriteString(runtime.output, disable); writeErr != nil {
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("disable mouse modes: %w", writeErr),
			)
		}
		if finalizeErr := finalizeInput(); finalizeErr != nil {
			cleanupErr = errors.Join(cleanupErr, finalizeErr)
		}
		if restoreErr := runtime.restore(runtime.controlFD, oldState); restoreErr != nil {
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("restore terminal: %w", restoreErr),
			)
		}
		return cleanupErr
	}

	serveErr, signalCode := runSignaledRawSession(
		cfg, input, runtime.output, enable, cleanup, signals,
	)
	if serveErr != nil {
		fmt.Fprintln(runtime.errorOutput, "mouse fixture:", serveErr)
		if signalCode != 0 {
			return signalCode
		}
		return 1
	}
	if signalCode != 0 {
		return signalCode
	}
	return 0
}

type onceReadCloser struct {
	readCloser
	once sync.Once
	err  error
}

func (c *onceReadCloser) Close() error {
	c.once.Do(func() {
		c.err = c.readCloser.Close()
	})
	return c.err
}

// runSignaledRawSession coordinates cancellation without sharing output or
// terminal cleanup with the signal worker. A signal already queued while
// MakeRaw was running is handled synchronously before the session starts.
func runSignaledRawSession(
	cfg config,
	input readCloser,
	output io.Writer,
	enable string,
	cleanup func() error,
	signals <-chan os.Signal,
) (error, int) {
	interrupted := make(chan struct{})
	signalExit := make(chan int, 1)
	sessionDone := make(chan struct{})
	workerDone := make(chan struct{})

	recordAndInterrupt := func(sig os.Signal) {
		if sig == os.Interrupt {
			signalExit <- 130
		} else {
			signalExit <- 1
		}
		close(interrupted)
		_ = input.Close()
	}

	workerStarted := false
	select {
	case sig := <-signals:
		recordAndInterrupt(sig)
	default:
		workerStarted = true
		go func() {
			defer close(workerDone)
			select {
			case sig := <-signals:
				recordAndInterrupt(sig)
			case <-sessionDone:
			}
		}()
	}

	serveErr := runRawSessionUntil(cfg, input, output, enable, cleanup, interrupted)
	close(sessionDone)
	if workerStarted {
		<-workerDone
	}

	select {
	case code := <-signalExit:
		return serveErr, code
	default:
		return serveErr, 0
	}
}

// runRawSession is the non-cancelled form used by parser and cleanup tests.
func runRawSession(
	cfg config,
	input io.Reader,
	output io.Writer,
	enable string,
	cleanup func(),
) error {
	return runRawSessionUntil(cfg, input, output, enable, func() error {
		cleanup()
		return nil
	}, nil)
}

// runRawSessionUntil is the sole owner of session output and cleanup. Its
// deferred cleanup therefore cannot overlap an enable, READY, or event write.
func runRawSessionUntil(
	cfg config,
	input io.Reader,
	output io.Writer,
	enable string,
	cleanup func() error,
	interrupted <-chan struct{},
) (sessionErr error) {
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil {
			sessionErr = errors.Join(sessionErr, cleanupErr)
		}
	}()

	if sessionInterrupted(interrupted) {
		return nil
	}
	if _, err := io.WriteString(output, enable+"READY\r\n"); err != nil {
		return fmt.Errorf("write READY: %w", err)
	}
	if sessionInterrupted(interrupted) {
		return nil
	}

	parser := newReportParser(cfg.format)
	buf := make([]byte, 1024)
	seen := 0
	for cfg.events == 0 || seen < cfg.events {
		n, readErr := input.Read(buf)
		if sessionInterrupted(interrupted) {
			return nil
		}
		if n > 0 {
			reports, parseErr := parser.feed(buf[:n])
			if parseErr != nil {
				return fmt.Errorf("parse: %w", parseErr)
			}
			for _, report := range reports {
				if sessionInterrupted(interrupted) {
					return nil
				}
				if _, err := fmt.Fprintf(output, "\r\n%s\r\n", report); err != nil {
					return fmt.Errorf("write event: %w", err)
				}
				seen++
				if cfg.events > 0 && seen >= cfg.events {
					break
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read: %w", readErr)
		}
	}
	return nil
}

func sessionInterrupted(interrupted <-chan struct{}) bool {
	if interrupted == nil {
		return false
	}
	select {
	case <-interrupted:
		return true
	default:
		return false
	}
}

func modeSequences(cfg config) (enable, disable string, err error) {
	trackingMode := map[string]int{
		"x10": 9, "normal": 1000, "button": 1002, "any": 1003,
	}[cfg.tracking]
	if trackingMode == 0 {
		return "", "", fmt.Errorf("unknown tracking mode %q", cfg.tracking)
	}
	formatMode := map[string]int{
		"x10": 0, "utf8": 1005, "sgr": 1006, "urxvt": 1015,
	}
	fm, ok := formatMode[cfg.format]
	if !ok {
		return "", "", fmt.Errorf("unknown mouse format %q", cfg.format)
	}
	var on, off strings.Builder
	fmt.Fprintf(&on, "\x1b[?%dh", trackingMode)
	fmt.Fprintf(&off, "\x1b[?%dl", trackingMode)
	if fm != 0 {
		fmt.Fprintf(&on, "\x1b[?%dh", fm)
		fmt.Fprintf(&off, "\x1b[?%dl", fm)
	}
	return on.String(), off.String(), nil
}

type reportParser struct {
	format string
	buf    []byte
}

func newReportParser(format string) *reportParser {
	return &reportParser{format: format}
}

func (p *reportParser) feed(chunk []byte) ([]string, error) {
	p.buf = append(p.buf, chunk...)
	var reports []string
	for len(p.buf) > 0 {
		n, event, complete, err := p.next()
		if err != nil {
			return nil, err
		}
		if !complete {
			break
		}
		p.buf = p.buf[n:]
		reports = append(reports, event)
	}
	return reports, nil
}

func (p *reportParser) next() (int, string, bool, error) {
	switch p.format {
	case "x10", "utf8":
		return p.nextLegacy()
	case "sgr", "urxvt":
		return p.nextCSI()
	default:
		return 0, "", false, fmt.Errorf("unsupported format %q", p.format)
	}
}

func (p *reportParser) nextLegacy() (int, string, bool, error) {
	const prefix = "\x1b[M"
	if len(p.buf) < len(prefix) {
		return 0, "", false, nil
	}
	if string(p.buf[:len(prefix)]) != prefix {
		return 0, "", false, fmt.Errorf("unexpected prefix %q", p.buf[:len(prefix)])
	}
	if len(p.buf) < 4 {
		return 0, "", false, nil
	}
	code := int(p.buf[3]) - 32
	if p.format == "x10" {
		if len(p.buf) < 6 {
			return 0, "", false, nil
		}
		return 6, formatReport(
			code, int(p.buf[4])-33, int(p.buf[5])-33, legacyRelease(code),
		), true, nil
	}

	x, xn, ok := decodeRune(p.buf[4:])
	if !ok {
		return 0, "", false, nil
	}
	y, yn, ok := decodeRune(p.buf[4+xn:])
	if !ok {
		return 0, "", false, nil
	}
	return 4 + xn + yn, formatReport(
		code, int(x)-33, int(y)-33, legacyRelease(code),
	), true, nil
}

func decodeRune(b []byte) (rune, int, bool) {
	if len(b) == 0 || !utf8.FullRune(b) {
		return 0, 0, false
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError && n == 1 {
		return 0, 0, false
	}
	return r, n, true
}

func (p *reportParser) nextCSI() (int, string, bool, error) {
	prefix := "\x1b["
	if p.format == "sgr" {
		prefix += "<"
	}
	if len(p.buf) < len(prefix) {
		return 0, "", false, nil
	}
	if string(p.buf[:len(prefix)]) != prefix {
		return 0, "", false, fmt.Errorf("unexpected prefix %q", p.buf[:len(prefix)])
	}
	end := -1
	for i := len(prefix); i < len(p.buf); i++ {
		if p.buf[i] == 'M' || p.buf[i] == 'm' {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, "", false, nil
	}
	parts := strings.Split(string(p.buf[len(prefix):end]), ";")
	if len(parts) != 3 {
		return 0, "", false, fmt.Errorf("invalid report %q", p.buf[:end+1])
	}
	values := [3]int{}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return 0, "", false, fmt.Errorf("invalid report number %q", part)
		}
		values[i] = value
	}
	code := values[0]
	if p.format == "urxvt" {
		code -= 32
	}
	release := p.buf[end] == 'm' || (p.format == "urxvt" && legacyRelease(code))
	return end + 1, formatReport(code, values[1]-1, values[2]-1, release), true, nil
}

func legacyRelease(code int) bool {
	return code&3 == 3 && code&32 == 0
}

func formatReport(code, x, y int, release bool) string {
	action := "press"
	button := "none"
	switch {
	case release:
		action = "release"
	case code&64 != 0:
		if code&1 == 0 {
			button = "wheel_up"
		} else {
			button = "wheel_down"
		}
	case code&32 != 0:
		action = "motion"
		button = baseButton(code & 3)
	default:
		button = baseButton(code & 3)
	}
	var modifiers []string
	if code&4 != 0 {
		modifiers = append(modifiers, "shift")
	}
	if code&8 != 0 {
		modifiers = append(modifiers, "alt")
	}
	if code&16 != 0 {
		modifiers = append(modifiers, "ctrl")
	}
	return fmt.Sprintf(
		"EVENT action=%s button=%s x=%d y=%d modifiers=%s",
		action, button, x, y, strings.Join(modifiers, ","),
	)
}

func baseButton(code int) string {
	switch code {
	case 0:
		return "left"
	case 1:
		return "middle"
	case 2:
		return "right"
	default:
		return "none"
	}
}
