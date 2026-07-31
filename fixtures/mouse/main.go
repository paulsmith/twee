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

	"golang.org/x/term"
)

type config struct {
	tracking string
	format   string
	events   int
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.tracking, "tracking", "any", "mouse tracking: x10, normal, button, any")
	flag.StringVar(&cfg.format, "format", "sgr", "mouse format: x10, utf8, sgr, urxvt")
	flag.IntVar(&cfg.events, "events", 0, "exit after this many reports (zero waits forever)")
	flag.Parse()

	enable, disable, err := modeSequences(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mouse fixture:", err)
		os.Exit(2)
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "mouse fixture: stdin is not a TTY")
		os.Exit(2)
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mouse fixture: raw mode:", err)
		os.Exit(1)
	}

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			_, _ = io.WriteString(os.Stdout, disable)
			_ = term.Restore(fd, oldState)
		})
	}
	defer cleanup()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		sig := <-signals
		cleanup()
		if sig == os.Interrupt {
			os.Exit(130)
		}
		os.Exit(1)
	}()

	if _, err := io.WriteString(os.Stdout, enable+"READY\r\n"); err != nil {
		fmt.Fprintln(os.Stderr, "mouse fixture: write READY:", err)
		os.Exit(1)
	}

	parser := newReportParser(cfg.format)
	buf := make([]byte, 1024)
	seen := 0
	for cfg.events == 0 || seen < cfg.events {
		n, readErr := os.Stdin.Read(buf)
		if n > 0 {
			reports, parseErr := parser.feed(buf[:n])
			if parseErr != nil {
				fmt.Fprintln(os.Stderr, "mouse fixture: parse:", parseErr)
				os.Exit(1)
			}
			for _, report := range reports {
				fmt.Fprintf(os.Stdout, "\r\n%s\r\n", report)
				seen++
				if cfg.events > 0 && seen >= cfg.events {
					break
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return
			}
			fmt.Fprintln(os.Stderr, "mouse fixture: read:", readErr)
			os.Exit(1)
		}
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
		return 6, formatReport(code, int(p.buf[4])-33, int(p.buf[5])-33, false), true, nil
	}

	x, xn, ok := decodeRune(p.buf[4:])
	if !ok {
		return 0, "", false, nil
	}
	y, yn, ok := decodeRune(p.buf[4+xn:])
	if !ok {
		return 0, "", false, nil
	}
	return 4 + xn + yn, formatReport(code, int(x)-33, int(y)-33, false), true, nil
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
	release := p.buf[end] == 'm' || (p.format == "urxvt" && code&3 == 3)
	return end + 1, formatReport(code, values[1]-1, values[2]-1, release), true, nil
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
