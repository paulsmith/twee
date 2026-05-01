package play

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Options controls playback.
type Options struct {
	Speed   float64
	Step    bool
	MaxIdle time.Duration
	Verbose bool

	Stdin  *os.File
	Stdout *os.File
	Stderr io.Writer

	SkipPreflight bool
	SkipRaw       bool
}

// Run plays path until the user quits, stdin closes, or an error occurs.
func Run(path string, opts Options) error {
	if path == "" {
		return fmt.Errorf("twee play: missing bundle path")
	}
	if opts.Speed == 0 {
		opts.Speed = 1
	}
	if !ValidSpeed(opts.Speed) {
		return fmt.Errorf("twee play: --speed must be > 0")
	}
	if opts.MaxIdle < 0 {
		return fmt.Errorf("twee play: --max-idle must be >= 0")
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

	pf := defaultPreflightOptions(opts.Stdin, opts.Stdout)
	terminalCols := 0
	if !opts.SkipPreflight {
		if err := checkStdoutTTY(pf); err != nil {
			return err
		}
		if width, _, err := pf.Term.GetSize(pf.StdoutFD); err == nil {
			terminalCols = width
		}
	}
	bundle, err := OpenBundle(path)
	if err != nil {
		return err
	}
	if !opts.SkipPreflight {
		if err := preflightBundle(bundle, pf); err != nil {
			return err
		}
	}
	if terminalCols <= 0 {
		terminalCols = bundle.MaxCols
	}

	rawRestore := func() {}
	if !opts.SkipRaw {
		old, err := realTerminalOps{}.MakeRaw(int(opts.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("twee play: raw mode: %w", err)
		}
		rawRestore = func() {
			_ = realTerminalOps{}.Restore(int(opts.Stdin.Fd()), old)
		}
	}
	if _, err := io.WriteString(opts.Stdout, "\x1b[?1049h\x1b[?25l\x1b[H"); err != nil {
		rawRestore()
		return err
	}
	restoreFn := func() {
		_, _ = io.WriteString(opts.Stdout, "\x1b[?25h\x1b[?1049l")
		rawRestore()
	}
	var restoreOnce sync.Once
	restore := func() { restoreOnce.Do(restoreFn) }
	stopSignals := installSignalRestore(restore)
	defer stopSignals()
	defer func() {
		if r := recover(); r != nil {
			restore()
			panic(r)
		}
	}()

	cmds := make(chan command, 16)
	go readCommands(opts.Stdin, cmds)

	l := newLoop(loopConfig{
		Events:  bundle.Events,
		Cols:    bundle.Manifest.Cols,
		Rows:    bundle.Manifest.Rows,
		Speed:   opts.Speed,
		MaxIdle: opts.MaxIdle,
		Step:    opts.Step,
		Cmds:    cmds,
		Sink:    newKittySink(opts.Stdout, terminalCols),
	})

	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()

	start := time.Now()
	for {
		now := time.Now()
		if l.tick(now) {
			break
		}
		<-ticker.C
	}
	restore()

	if l.err != nil {
		return l.err
	}
	if opts.Verbose {
		fmt.Fprintf(opts.Stderr, "twee play: played %d events from %s in %s\n",
			len(bundle.Events), path, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

// ValidSpeed reports whether v is an acceptable playback speed multiplier.
func ValidSpeed(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func installSignalRestore(restore func()) func() {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case sig := <-sigCh:
			restore()
			if s, ok := sig.(syscall.Signal); ok {
				os.Exit(128 + int(s))
			}
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}
