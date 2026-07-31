// Package pump owns the read loop from the PTY and feeds the VT model.
//
// Synchronization model:
//
//   - One goroutine (the pump) owns all reads from the master.
//   - All access to the VT model is guarded by mu.
//   - After every Feed, gen is incremented and cond.Broadcast wakes
//     waiters.
//   - Wait blocks until either pred(snapshot) is true, the timeout
//     fires, or the model is closed.
package pump

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/vt"
)

// Pump drives a vt.Model from an io.Reader.
type Pump struct {
	model  vt.Model
	reader io.Reader

	mu     sync.Mutex
	cond   *sync.Cond
	gen    uint64
	closed bool

	// recent holds the last N output bytes for diagnostics.
	recent []byte

	// last receive time and whether any feed has occurred. lastFeed is
	// only meaningful when gotAnyFeed is true; WaitStable refuses to
	// declare stability before then to avoid first-paint races.
	lastFeed   time.Time
	gotAnyFeed bool

	// Recorder hook. Called outside mu to avoid blocking the pump on
	// recorder I/O.
	onOutput func(bytes []byte, t time.Time)
}

// New constructs a Pump. The caller must call Run in a goroutine.
func New(model vt.Model, r io.Reader) *Pump {
	p := &Pump{
		model:  model,
		reader: r,
		recent: make([]byte, 0, 64*1024),
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// SetOutputHook installs a function called under mu after each Feed.
// Used by the recorder.
func (p *Pump) SetOutputHook(fn func([]byte, time.Time)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onOutput = fn
}

// Run reads until EOF or error. It is intended to run in its own
// goroutine. After Run returns, no further model mutations occur.
func (p *Pump) Run() error {
	buf := make([]byte, 32*1024)
	for {
		n, err := p.reader.Read(buf)
		if n > 0 {
			// Copy the chunk before releasing the lock so the hook can
			// outlive the buffer reuse on the next Read.
			chunk := append([]byte(nil), buf[:n]...)
			p.mu.Lock()
			_ = p.model.Feed(chunk)
			p.appendRecent(chunk)
			now := time.Now()
			p.lastFeed = now
			p.gotAnyFeed = true
			p.gen++
			hook := p.onOutput
			p.cond.Broadcast()
			p.mu.Unlock()
			// Hook runs outside mu so a slow recorder cannot stall
			// snapshots or waiters.
			if hook != nil {
				hook(chunk, now)
			}
		}
		if err != nil {
			p.mu.Lock()
			p.closed = true
			p.gen++
			p.cond.Broadcast()
			p.mu.Unlock()
			if isExpectedEOF(err) {
				return nil
			}
			return err
		}
	}
}

func isExpectedEOF(err error) bool {
	if err == io.EOF {
		return true
	}
	// On Linux, reading from a PTY master after the slave closes
	// returns EIO. Treat that as normal end-of-stream.
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// syscall.EIO check: do it by string to avoid pulling syscall in here.
	if err != nil && err.Error() != "" && (containsAny(err.Error(),
		"input/output error", "i/o error on closed pty", "file already closed")) {
		return true
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func (p *Pump) appendRecent(chunk []byte) {
	const cap = 64 * 1024
	if len(p.recent)+len(chunk) <= cap {
		p.recent = append(p.recent, chunk...)
		return
	}
	// Drop oldest.
	keep := cap - len(chunk)
	if keep < 0 {
		p.recent = append(p.recent[:0], chunk[len(chunk)-cap:]...)
		return
	}
	p.recent = append(p.recent[:0], p.recent[len(p.recent)-keep:]...)
	p.recent = append(p.recent, chunk...)
}

// Snapshot returns a fresh snapshot.
func (p *Pump) Snapshot() vt.Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model.Snapshot()
}

// RecentBytes returns a copy of the most recent output for diagnostics.
func (p *Pump) RecentBytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]byte, len(p.recent))
	copy(out, p.recent)
	return out
}

// Resize forwards a resize to the model under mu.
func (p *Pump) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.model.Resize(cols, rows)
	p.gen++
	p.cond.Broadcast()
	return err
}

// EncodeMouse inspects the current model state and preflights/encodes an
// entire normalized mouse event batch while holding the model mutex. It
// returns bytes but never writes them to the PTY.
func (p *Pump) EncodeMouse(events []input.MouseEvent) (vt.MouseEncodingResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	model, ok := p.model.(vt.MouseModel)
	if !ok {
		return vt.MouseEncodingResult{}, &vt.MouseEncodeError{
			Reason: vt.MouseErrorUnsupportedBackend,
		}
	}
	return model.EncodeMouse(events)
}

// MouseState returns the mouse capability's state under the same mutex used
// for Feed and encoding. It is intended for truthful mode queries; callers
// must check TrackingKnown and FormatKnown before publishing derived values.
func (p *Pump) MouseState() (vt.MouseState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	model, ok := p.model.(vt.MouseModel)
	if !ok {
		return vt.MouseState{}, &vt.MouseEncodeError{
			Reason: vt.MouseErrorUnsupportedBackend,
		}
	}
	return model.MouseState()
}

// Wait blocks until pred(snapshot) returns true, the deadline fires,
// or the pump closes. pred is evaluated under mu after every model
// change.
func (p *Pump) Wait(ctx context.Context, timeout time.Duration, pred func(vt.Snapshot) bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pred(p.model.Snapshot()) {
		return nil
	}
	if p.closed {
		return ErrClosed
	}

	deadline := time.Now().Add(timeout)
	stop := make(chan struct{})
	defer close(stop)

	// Wake the cond at deadline or on context cancel.
	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-stop:
		case <-t.C:
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		case <-ctx.Done():
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		}
	}()

	for {
		p.cond.Wait()
		if pred(p.model.Snapshot()) {
			return nil
		}
		if p.closed {
			if pred(p.model.Snapshot()) {
				return nil
			}
			return ErrClosed
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return ErrTimeout
		}
	}
}

// WaitStable blocks until at least quietFor elapses with no new output,
// or the timeout fires. Refuses to declare stability before the first
// feed has been observed (or the pump has closed) — otherwise a call
// made immediately after Run would return on an empty screen if the
// app's first paint is later than quietFor.
func (p *Pump) WaitStable(ctx context.Context, quietFor, timeout time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	deadline := time.Now().Add(timeout)
	stop := make(chan struct{})
	defer close(stop)

	// Single timer goroutine; we Reset rather than spawn-per-iteration.
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-timer.C:
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
			case <-ctx.Done():
				p.mu.Lock()
				p.cond.Broadcast()
				p.mu.Unlock()
				return
			}
		}
	}()

	for {
		now := time.Now()
		stable := p.gotAnyFeed && now.Sub(p.lastFeed) >= quietFor
		if stable || p.closed {
			return nil
		}
		// Compute the next interesting wakeup: either when the quiet
		// window completes (relative to the last feed if any, else
		// the deadline) or the deadline itself.
		var wakeIn time.Duration
		if p.gotAnyFeed {
			wakeIn = quietFor - now.Sub(p.lastFeed)
		} else {
			wakeIn = deadline.Sub(now)
		}
		if rem := deadline.Sub(now); rem < wakeIn {
			wakeIn = rem
		}
		if wakeIn <= 0 {
			return ErrTimeout
		}
		timer.Reset(wakeIn)
		p.cond.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			// Final re-check before declaring timeout.
			if p.gotAnyFeed && time.Since(p.lastFeed) >= quietFor {
				return nil
			}
			return ErrTimeout
		}
	}
}

// Errors.
var (
	ErrTimeout = errors.New("pump: timeout")
	ErrClosed  = errors.New("pump: closed")
)
