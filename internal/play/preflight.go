package play

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	kittyQuery        = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	iterm2Query       = "\x1b]1337;Capabilities\x1b\\"
	primaryDAQuery    = "\x1b[c"
	maxProbeReplySize = 16 << 10
)

type terminalOps interface {
	IsTerminal(fd int) bool
	GetSize(fd int) (width, height int, err error)
	MakeRaw(fd int) (*term.State, error)
	Restore(fd int, oldState *term.State) error
}

type realTerminalOps struct{}

func (realTerminalOps) IsTerminal(fd int) bool { return term.IsTerminal(fd) }
func (realTerminalOps) GetSize(fd int) (int, int, error) {
	return term.GetSize(fd)
}
func (realTerminalOps) MakeRaw(fd int) (*term.State, error) { return term.MakeRaw(fd) }
func (realTerminalOps) Restore(fd int, old *term.State) error {
	return term.Restore(fd, old)
}

type preflightOptions struct {
	StdinFD  int
	StdoutFD int
	In       io.Reader
	Out      io.Writer
	Term     terminalOps
	Timeout  time.Duration
	Getenv   func(string) string
	Pixels   displayPixels
}

func defaultPreflightOptions(stdin, stdout *os.File) preflightOptions {
	return preflightOptions{
		StdinFD:  int(stdin.Fd()),
		StdoutFD: int(stdout.Fd()),
		In:       stdin,
		Out:      stdout,
		Term:     realTerminalOps{},
		Timeout:  200 * time.Millisecond,
		Getenv:   os.Getenv,
	}
}

func checkStdoutTTY(opts preflightOptions) error {
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if !opts.Term.IsTerminal(opts.StdoutFD) {
		return fmt.Errorf("twee play: refusing to play to a non-tty")
	}
	return nil
}

func preflightForBackend(opts preflightOptions, requested Backend) (Backend, terminalSize, error) {
	if err := checkStdoutTTY(opts); err != nil {
		return "", terminalSize{}, err
	}
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	width, height, err := opts.Term.GetSize(opts.StdoutFD)
	if err != nil {
		return "", terminalSize{}, fmt.Errorf("twee play: terminal size: %w", err)
	}
	if width < 1 || height < 3 {
		return "", terminalSize{}, fmt.Errorf("twee play: terminal is %dx%d; playback needs at least 1x3",
			width, height)
	}
	backend, err := selectBackend(opts, requested)
	if err != nil {
		return "", terminalSize{}, err
	}
	return backend, terminalSize{Cols: width, Rows: height}, nil
}

type backendSupport struct {
	kitty         bool
	iterm         bool
	itermReported bool
	itermIdentity bool
	sixel         bool
}

func selectBackend(opts preflightOptions, requested Backend) (Backend, error) {
	if opts.Getenv == nil {
		opts.Getenv = func(string) string { return "" }
	}
	if opts.Getenv("TMUX") != "" || opts.Getenv("STY") != "" {
		return "", fmt.Errorf("twee play: graphics playback through a terminal multiplexer is not supported; use a direct terminal")
	}
	if requested == BackendSixel && (opts.Pixels.Width <= 0 || opts.Pixels.Height <= 0) {
		return "", fmt.Errorf("twee play: sixel backend requires reliable terminal pixel geometry")
	}
	support, err := probeBackends(opts, requested)
	if err != nil {
		return "", err
	}
	available := func(backend Backend) bool {
		switch backend {
		case BackendKitty:
			return support.kitty
		case BackendITerm2:
			return support.iterm
		case BackendSixel:
			return support.sixel && opts.Pixels.Width > 0 && opts.Pixels.Height > 0
		default:
			return false
		}
	}
	if requested != BackendAuto {
		if available(requested) {
			return requested, nil
		}
		switch requested {
		case BackendKitty:
			return "", fmt.Errorf("twee play: kitty backend unavailable: graphics protocol not detected")
		case BackendITerm2:
			return "", fmt.Errorf("twee play: iterm2 backend unavailable: inline-image (FILE) capability not detected")
		case BackendSixel:
			return "", fmt.Errorf("twee play: sixel backend unavailable: Sixel capability not detected")
		}
	}
	for _, backend := range []Backend{BackendKitty, BackendITerm2, BackendSixel} {
		if available(backend) {
			return backend, nil
		}
	}
	sixelReason := "capability not detected"
	if support.sixel && (opts.Pixels.Width <= 0 || opts.Pixels.Height <= 0) {
		sixelReason = "terminal pixel geometry unavailable"
	}
	itermReason := "iTerm identity not detected"
	if support.itermReported && !support.itermIdentity {
		itermReason = "ambiguous F capability without iTerm identity"
	}
	return "", fmt.Errorf("twee play: no usable graphics backend (kitty: protocol not detected; iterm2: %s; sixel: %s)", itermReason, sixelReason)
}

func probeBackends(opts preflightOptions, requested Backend) (backendSupport, error) {
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 200 * time.Millisecond
	}
	old, err := opts.Term.MakeRaw(opts.StdinFD)
	if err != nil {
		return backendSupport{}, fmt.Errorf("twee play: raw mode: %w", err)
	}
	defer opts.Term.Restore(opts.StdinFD, old)

	queries := probeQueries(requested)
	if _, err := io.WriteString(opts.Out, queries); err != nil {
		return backendSupport{}, fmt.Errorf("twee play: graphics capability query: %w", err)
	}
	reply, err := readProbeReplies(opts.In, opts.Timeout, requested)
	if err != nil {
		return backendSupport{}, fmt.Errorf("twee play: graphics capability reply: %w", err)
	}
	support := parseBackendSupport(reply)

	// TERM_FEATURES is the feature-reporting protocol's official alternate
	// publication mechanism. Terminal-specific variables are weaker fallbacks
	// for older direct terminals that implement graphics but not reporting.
	features := parseFeatureString(opts.Getenv("TERM_FEATURES"))
	support.itermReported = support.itermReported || features["F"]
	support.itermIdentity = opts.Getenv("TERM_PROGRAM") == "iTerm.app" || opts.Getenv("ITERM_SESSION_ID") != ""
	if requested == BackendITerm2 {
		// An explicit backend is the escape hatch for terminals that implement
		// OSC 1337 without publishing an iTerm identity. Auto-selection cannot
		// use F alone because the published feature table also assigns F to
		// focus reporting.
		support.iterm = support.itermReported || support.itermIdentity
	} else {
		support.iterm = support.itermIdentity
	}
	support.sixel = support.sixel || features["Sx"]
	support.kitty = support.kitty || (opts.Getenv("KITTY_WINDOW_ID") != "" && strings.Contains(opts.Getenv("TERM"), "kitty"))
	return support, nil
}

func probeQueries(requested Backend) string {
	switch requested {
	case BackendKitty:
		// Kitty recommends following the graphics query with primary DA.
		return kittyQuery + primaryDAQuery
	case BackendITerm2:
		return iterm2Query + primaryDAQuery
	case BackendSixel:
		return iterm2Query + primaryDAQuery
	default:
		return kittyQuery + iterm2Query + primaryDAQuery
	}
}

func readProbeReplies(r io.Reader, timeout time.Duration, requested Backend) ([]byte, error) {
	if fdReader, ok := r.(interface{ Fd() uintptr }); ok {
		// Run always reaches this path with its *os.File stdin. Polling and
		// reading synchronously guarantees that capability detection relinquishes
		// the fd before readCommands starts, even when os.File deadlines are not
		// supported by the terminal device.
		return readProbeFD(int(fdReader.Fd()), timeout, requested)
	}
	recorder := &recordingReader{r: r}
	deadline, hasDeadline := r.(interface{ SetReadDeadline(time.Time) error })
	if hasDeadline {
		hasDeadline = deadline.SetReadDeadline(time.Now().Add(timeout)) == nil
	}
	type result struct {
		reply []byte
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := readProbeStream(recorder, maxProbeReplySize, requested)
		ch <- result{reply: data, err: normalizeProbeReadError(err)}
	}()
	if hasDeadline {
		// A successful deadline contract makes this a bounded embedder path.
		// Wait until the read has actually exited before clearing the deadline;
		// otherwise a stale probe goroutine can steal playback keystrokes from
		// readCommands. The reader deadline keeps this wait bounded.
		result := <-ch
		if err := deadline.SetReadDeadline(time.Time{}); err != nil {
			return result.reply, errors.Join(result.err, fmt.Errorf("clear probe read deadline: %w", err))
		}
		return result.reply, result.err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-ch:
		return result.reply, result.err
	case <-timer.C:
		// Return bytes accumulated even when auto-detection is waiting for
		// primary DA as an ordering fence. Capability-only terminals may not
		// answer DA, but their complete OSC report is still authoritative.
		return recorder.bytes(), nil
	}
}

func readProbeFD(fd int, timeout time.Duration, requested Backend) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var out bytes.Buffer
	var buf [1]byte
	for out.Len() < maxProbeReplySize {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return out.Bytes(), nil
		}
		waitMS := int((remaining + time.Millisecond - 1) / time.Millisecond)
		pollFD := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pollFD, waitMS)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return out.Bytes(), err
		}
		if n == 0 {
			return out.Bytes(), nil
		}
		n, err = unix.Read(fd, buf[:])
		if n > 0 {
			out.Write(buf[:n])
			if probeRepliesComplete(out.Bytes(), requested) {
				return out.Bytes(), nil
			}
		}
		switch err {
		case nil:
			if n == 0 {
				return out.Bytes(), nil
			}
		case unix.EINTR, unix.EAGAIN:
			continue
		default:
			return out.Bytes(), err
		}
	}
	return out.Bytes(), fmt.Errorf("reply too large")
}

func normalizeProbeReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) {
		return nil
	}
	return err
}

// recordingReader preserves partial probe replies for the timeout path. A
// generic io.Reader cannot be canceled, so its goroutine may live until that
// reader unblocks. Run's terminal *os.File takes the synchronous fd-poll path
// above and never leaves a competing reader behind.
type recordingReader struct {
	r    io.Reader
	mu   sync.Mutex
	data []byte
}

func (r *recordingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.data = append(r.data, p[:n]...)
		r.mu.Unlock()
	}
	return n, err
}

func (r *recordingReader) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.data...)
}

func readProbeStream(r io.Reader, limit int, requested Backend) ([]byte, error) {
	var out bytes.Buffer
	var b [1]byte
	for out.Len() < limit {
		n, err := r.Read(b[:])
		if n > 0 {
			out.WriteByte(b[0])
			if probeRepliesComplete(out.Bytes(), requested) {
				return out.Bytes(), nil
			}
		}
		if err != nil {
			return out.Bytes(), err
		}
	}
	return out.Bytes(), fmt.Errorf("reply too large")
}

func probeRepliesComplete(reply []byte, _ Backend) bool {
	// Every backend query is followed by primary DA. Consume through that
	// ordered fence so its reply cannot leak into interactive input; if a
	// terminal omits DA, timeout returns any complete capability reply seen.
	return hasPrimaryDAReply(reply)
}

func parseBackendSupport(reply []byte) backendSupport {
	text := string(reply)
	support := backendSupport{
		kitty: strings.Contains(text, "\x1b_Gi=31;OK\x1b\\"),
		sixel: primaryDAHasSixel(reply),
	}
	for _, featureString := range capabilityReplies(reply) {
		features := parseFeatureString(featureString)
		support.itermReported = support.itermReported || features["F"]
		support.sixel = support.sixel || features["Sx"]
	}
	return support
}

func capabilityReplies(reply []byte) []string {
	const prefix = "\x1b]1337;Capabilities="
	var out []string
	for rest := string(reply); ; {
		i := strings.Index(rest, prefix)
		if i < 0 {
			return out
		}
		rest = rest[i+len(prefix):]
		end := strings.IndexByte(rest, '\a')
		terminatorLen := 1
		if st := strings.Index(rest, "\x1b\\"); st >= 0 && (end < 0 || st < end) {
			end = st
			terminatorLen = 2
		}
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end+terminatorLen:]
	}
}

func parseFeatureString(s string) map[string]bool {
	features := make(map[string]bool)
	for i := 0; i < len(s); {
		if s[i] < 'A' || s[i] > 'Z' {
			break
		}
		start := i
		i++
		for i < len(s) && s[i] >= 'a' && s[i] <= 'z' {
			i++
		}
		nameEnd := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		features[s[start:nameEnd]] = true
	}
	return features
}

func primaryDAHasSixel(reply []byte) bool {
	for rest := string(reply); ; {
		i := strings.Index(rest, "\x1b[?")
		if i < 0 {
			return false
		}
		rest = rest[i+3:]
		end := strings.IndexByte(rest, 'c')
		if end < 0 {
			return false
		}
		params := rest[:end]
		valid := params != ""
		for _, field := range strings.Split(params, ";") {
			if field == "4" {
				return true
			}
			for _, c := range field {
				if c < '0' || c > '9' {
					valid = false
				}
			}
		}
		if valid {
			rest = rest[end+1:]
		} else {
			rest = rest[1:]
		}
	}
}

func hasPrimaryDAReply(reply []byte) bool {
	for rest := string(reply); ; {
		i := strings.Index(rest, "\x1b[?")
		if i < 0 {
			return false
		}
		rest = rest[i+3:]
		end := strings.IndexByte(rest, 'c')
		if end < 0 {
			return false
		}
		valid := rest[:end] != ""
		for _, c := range rest[:end] {
			if (c < '0' || c > '9') && c != ';' {
				valid = false
				break
			}
		}
		if valid {
			return true
		}
		rest = rest[1:]
	}
}
