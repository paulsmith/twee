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
	Backend Backend

	// DisableMouseAnnotations suppresses the transient visual feedback drawn
	// for recorded semantic mouse input. The zero value leaves it enabled.
	DisableMouseAnnotations bool

	// DisplayPixelWidth and DisplayPixelHeight are the host terminal's native
	// pixel dimensions. When set, frames are rendered to the pixel size of
	// their graphics placement area instead of the renderer's default font size.
	DisplayPixelWidth  int
	DisplayPixelHeight int

	Stdin  *os.File
	Stdout *os.File
	Stderr io.Writer

	SkipPreflight bool
	SkipRaw       bool

	// sink is an internal lifecycle test hook. Production callers use the
	// backend-selected sink constructed by Run.
	sink playbackSink
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
	if opts.Backend == "" {
		opts.Backend = BackendAuto
	}
	if !ValidBackend(opts.Backend) {
		return fmt.Errorf("twee play: invalid backend %q (want auto, kitty, iterm2, or sixel)", opts.Backend)
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
	pf.Pixels = displayPixels{Width: opts.DisplayPixelWidth, Height: opts.DisplayPixelHeight}
	terminal := terminalSize{}
	backend := opts.Backend
	bundle, err := OpenBundle(path)
	if err != nil {
		return err
	}
	if !opts.SkipPreflight {
		backend, terminal, err = preflightForBackend(pf, opts.Backend)
		if err != nil {
			return err
		}
	} else if backend == BackendAuto {
		// Test and embedding callers that deliberately bypass capability
		// detection retain the historical Kitty behavior.
		backend = BackendKitty
	}
	terminalCols := terminal.Cols
	terminalRows := terminal.Rows
	if terminalCols <= 0 {
		terminalCols = bundle.MaxCols
		if terminalCols < 1 {
			terminalCols = 1
		}
	}
	if terminalRows <= 0 {
		terminalRows = bundle.MaxRows + 2
		if terminalRows < 3 {
			terminalRows = 3
		}
	}

	sink := opts.sink
	if sink == nil {
		sink, err = newFrameSink(backend, opts.Stdout, terminalCols, displayPixels{
			Width: opts.DisplayPixelWidth, Height: opts.DisplayPixelHeight,
		})
		if err != nil {
			return err
		}
	}

	rawRestore := func() {}
	if !opts.SkipRaw {
		old, err := realTerminalOps{}.MakeRaw(int(opts.Stdin.Fd()))
		if err != nil {
			_ = sink.Close()
			return fmt.Errorf("twee play: raw mode: %w", err)
		}
		rawRestore = func() {
			_ = realTerminalOps{}.Restore(int(opts.Stdin.Fd()), old)
		}
	}
	if _, err := io.WriteString(opts.Stdout, "\x1b[?1049h\x1b[?25l\x1b[H"); err != nil {
		_ = sink.Close()
		rawRestore()
		return err
	}
	var closeErr error
	restoreFn := func() {
		closeErr = sink.Close()
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
		Sink:    sink,
		DisplayPixels: displayPixels{
			Width:  opts.DisplayPixelWidth,
			Height: opts.DisplayPixelHeight,
		},
		TerminalSize: terminalSize{
			Cols: terminalCols,
			Rows: terminalRows,
		},
		DisableMouseAnnotations: opts.DisableMouseAnnotations,
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
	if closeErr != nil {
		return fmt.Errorf("twee play: close %s backend: %w", backend, closeErr)
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
