package play

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

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

// preflightBundle retains the Kitty-only preflight used by older package
// tests and embedders. Run uses preflightBundleForBackend for selection.
func preflightBundle(bundle Bundle, opts preflightOptions) error {
	_, err := preflightBundleForBackend(bundle, opts, BackendKitty)
	return err
}

func preflightBundleForBackend(bundle Bundle, opts preflightOptions, requested Backend) (Backend, error) {
	if err := checkStdoutTTY(opts); err != nil {
		return "", err
	}
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	width, height, err := opts.Term.GetSize(opts.StdoutFD)
	if err != nil {
		return "", fmt.Errorf("twee play: terminal size: %w", err)
	}
	needCols, needRows := bundle.MaxCols, bundle.MaxRows+2
	if needCols < 1 {
		needCols = 1
	}
	if needRows < 3 {
		needRows = 3
	}
	if width < needCols || height < needRows {
		return "", fmt.Errorf("twee play: terminal is %dx%d; trace needs at least %dx%d",
			width, height, needCols, needRows)
	}
	return selectBackend(opts, requested)
}

type backendSupport struct {
	kitty bool
	iterm bool
	sixel bool
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
	return "", fmt.Errorf("twee play: no usable graphics backend (kitty: protocol not detected; iterm2: FILE capability not detected; sixel: %s)", sixelReason)
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
	reply := readProbeReplies(opts.In, opts.Timeout, requested)
	support := parseBackendSupport(reply)

	// TERM_FEATURES is the feature-reporting protocol's official alternate
	// publication mechanism. Terminal-specific variables are weaker fallbacks
	// for older direct terminals that implement graphics but not reporting.
	features := parseFeatureString(opts.Getenv("TERM_FEATURES"))
	support.iterm = support.iterm || features["F"] || opts.Getenv("TERM_PROGRAM") == "iTerm.app"
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
		return iterm2Query
	case BackendSixel:
		return iterm2Query + primaryDAQuery
	default:
		return kittyQuery + iterm2Query + primaryDAQuery
	}
}

func readProbeReplies(r io.Reader, timeout time.Duration, requested Backend) []byte {
	recorder := &recordingReader{r: r}
	deadline, hasDeadline := r.(interface{ SetReadDeadline(time.Time) error })
	if hasDeadline {
		hasDeadline = deadline.SetReadDeadline(time.Now().Add(timeout)) == nil
	}
	type result struct{ reply []byte }
	ch := make(chan result, 1)
	go func() {
		data, _ := readProbeStream(recorder, maxProbeReplySize, requested)
		ch <- result{reply: data}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-ch:
		if hasDeadline {
			_ = deadline.SetReadDeadline(time.Time{})
		}
		return result.reply
	case <-timer.C:
		// Return bytes accumulated even when auto-detection is waiting for
		// primary DA as an ordering fence. Capability-only terminals may not
		// answer DA, but their complete OSC report is still authoritative.
		if hasDeadline {
			// Force any read that raced the timer to finish before removing the
			// deadline, so the playback input reader cannot inherit it.
			_ = deadline.SetReadDeadline(time.Now())
			select {
			case result := <-ch:
				_ = deadline.SetReadDeadline(time.Time{})
				return result.reply
			case <-time.After(20 * time.Millisecond):
				_ = deadline.SetReadDeadline(time.Time{})
			}
		}
		return recorder.bytes()
	}
}

// recordingReader preserves partial probe replies for the timeout path. A
// generic io.Reader cannot be canceled, so its goroutine may live until that
// reader unblocks; real terminal files support deadlines and take the bounded
// path above.
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

func probeRepliesComplete(reply []byte, requested Backend) bool {
	switch requested {
	case BackendKitty:
		return bytes.Contains(reply, []byte("\x1b_Gi=31;")) && bytes.Contains(reply, []byte("\x1b\\")) ||
			hasPrimaryDAReply(reply)
	case BackendITerm2:
		return len(capabilityReplies(reply)) > 0
	case BackendSixel:
		for _, features := range capabilityReplies(reply) {
			if parseFeatureString(features)["Sx"] {
				return true
			}
		}
		return hasPrimaryDAReply(reply)
	default:
		// Primary DA is the ordered fence recommended by Kitty: once it
		// arrives, preceding supported graphics queries have replied. A
		// successful Kitty response can short-circuit because it is auto's
		// highest-priority backend.
		return bytes.Contains(reply, []byte("\x1b_Gi=31;OK\x1b\\")) || hasPrimaryDAReply(reply)
	}
}

func parseBackendSupport(reply []byte) backendSupport {
	text := string(reply)
	support := backendSupport{
		kitty: strings.Contains(text, "\x1b_Gi=31;OK\x1b\\"),
		sixel: primaryDAHasSixel(reply),
	}
	for _, featureString := range capabilityReplies(reply) {
		features := parseFeatureString(featureString)
		// The published feature table currently assigns F to both focus
		// reporting and FILE. There is no wire-level way to distinguish them;
		// treat advertised F as FILE, as iTerm2 itself prescribes for images.
		support.iterm = support.iterm || features["F"]
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

// queryKitty is kept as a focused compatibility probe for package callers and
// tests. New auto-detection uses the combined bounded probe above.
func queryKitty(opts preflightOptions) error {
	if opts.Term == nil {
		opts.Term = realTerminalOps{}
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 200 * time.Millisecond
	}
	old, err := opts.Term.MakeRaw(opts.StdinFD)
	if err != nil {
		return fmt.Errorf("twee play: raw mode: %w", err)
	}
	defer opts.Term.Restore(opts.StdinFD, old)
	if _, err := io.WriteString(opts.Out, kittyQuery); err != nil {
		return fmt.Errorf("twee play: kitty query: %w", err)
	}
	if d, ok := opts.In.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(time.Now().Add(opts.Timeout))
		defer d.SetReadDeadline(time.Time{})
	}
	type result struct {
		reply []byte
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		reply, err := readAPCReply(opts.In, 4096)
		ch <- result{reply: reply, err: err}
	}()
	select {
	case res := <-ch:
		if res.err != nil || !bytes.Contains(res.reply, []byte("\x1b_Gi=31;OK\x1b\\")) {
			return fmt.Errorf("twee play: kitty graphics protocol not detected")
		}
		return nil
	case <-time.After(opts.Timeout):
		return fmt.Errorf("twee play: kitty graphics protocol not detected")
	}
}

func readAPCReply(r io.Reader, limit int) ([]byte, error) {
	var out bytes.Buffer
	var b [1]byte
	for out.Len() < limit {
		n, err := r.Read(b[:])
		if n > 0 {
			out.WriteByte(b[0])
			if bytes.HasSuffix(out.Bytes(), []byte("\x1b\\")) {
				return out.Bytes(), nil
			}
		}
		if err != nil {
			return out.Bytes(), err
		}
	}
	return out.Bytes(), fmt.Errorf("reply too large")
}
