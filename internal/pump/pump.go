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
	"strings"
	"sync"
	"time"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/vt"
)

var ErrPresentationUnavailable = errors.New("terminal backend does not expose input presentation state")
var ErrKittyKeyboardStateUnknown = errors.New("terminal backend does not expose Kitty keyboard protocol state")
var ErrKittyKeyboardUnsupported = errors.New("semantic keys are unsupported while the Kitty keyboard protocol is active")

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

// Rect is a rectangle of terminal cells to omit from a stable-screen
// comparison. Coordinates are clipped to each snapshot's live viewport.
type Rect struct {
	X, Y int
	W, H int
}

// Capture is one coherent observation of pump-owned terminal state.
type Capture struct {
	CapturedAt      time.Time
	Generation      uint64
	Snapshot        vt.Snapshot
	Presentation    vt.Presentation
	PresentationErr error
	Mouse           vt.MouseState
	MouseErr        error
	RecentOutput    []byte
	Closed          bool
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
	if errors.Is(err, io.EOF) {
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
		if strings.Contains(s, sub) {
			return true
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

// Capture returns one coherent copy of the current terminal state.
func (p *Pump) Capture() Capture {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.captureLocked()
}

func (p *Pump) captureLocked() Capture {
	capture := Capture{
		CapturedAt:   time.Now(),
		Generation:   p.gen,
		Snapshot:     p.model.Snapshot(),
		RecentOutput: append([]byte(nil), p.recent...),
		Closed:       p.closed,
	}
	capture.Presentation, capture.PresentationErr = presentationOf(p.model)
	if model, ok := p.model.(vt.MouseModel); ok {
		capture.Mouse, capture.MouseErr = model.MouseState()
	} else {
		capture.MouseErr = &vt.MouseEncodeError{Reason: vt.MouseErrorUnsupportedBackend}
	}
	return capture
}

// Presentation returns the current host-relevant terminal state under the
// same mutex used by Feed. Callers therefore observe the VT model's source of
// truth rather than a separately maintained mode approximation.
func (p *Pump) Presentation() (vt.Presentation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return presentationOf(p.model)
}

// EncodeKey encodes a semantic key from the current VT input modes while
// holding the model mutex. It returns bytes but never writes to the PTY.
func (p *Pump) EncodeKey(k input.Key) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	presentation, err := presentationOf(p.model)
	if err != nil {
		return nil, err
	}
	if !presentation.Input.KittyKeyboardKnown {
		return nil, ErrKittyKeyboardStateUnknown
	}
	if presentation.Input.KittyKeyboardFlags != 0 {
		return nil, ErrKittyKeyboardUnsupported
	}
	return input.EncodeWithModes(k, input.KeyModes{
		ApplicationCursor: presentation.Input.ApplicationCursor,
	}), nil
}

func presentationOf(model vt.Model) (vt.Presentation, error) {
	source, ok := model.(vt.PresentationSource)
	if !ok {
		return vt.Presentation{}, ErrPresentationUnavailable
	}
	return source.Presentation()
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
	return p.encodeMouseLocked(events)
}

// encodeMouseLocked encodes a normalized mouse batch. The caller holds p.mu.
func (p *Pump) encodeMouseLocked(events []input.MouseEvent) (vt.MouseEncodingResult, error) {
	model, ok := p.model.(vt.MouseModel)
	if !ok {
		return vt.MouseEncodingResult{}, &vt.MouseEncodeError{
			Reason: vt.MouseErrorUnsupportedBackend,
		}
	}
	return model.EncodeMouse(events)
}

// EncodeMouseSnapshot builds and encodes a mouse batch while holding the same
// model lock across snapshot inspection and encoding. The callback must be
// pure with respect to Pump and must not perform I/O.
func (p *Pump) EncodeMouseSnapshot(build func(vt.Snapshot) ([]input.MouseEvent, error)) (vt.MouseEncodingResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	events, err := build(p.model.Snapshot())
	if err != nil {
		return vt.MouseEncodingResult{}, err
	}
	return p.encodeMouseLocked(events)
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
	_, err := p.WaitCapture(ctx, timeout, pred)
	return err
}

// WaitCapture is Wait with the final evaluated terminal capture.
func (p *Pump) WaitCapture(ctx context.Context, timeout time.Duration, pred func(vt.Snapshot) bool) (Capture, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pred(p.model.Snapshot()) {
		return p.captureLocked(), nil
	}
	if p.closed {
		return p.captureLocked(), ErrClosed
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
			return p.captureLocked(), nil
		}
		if p.closed {
			if pred(p.model.Snapshot()) {
				return p.captureLocked(), nil
			}
			return p.captureLocked(), ErrClosed
		}
		if ctx.Err() != nil {
			return p.captureLocked(), ctx.Err()
		}
		if time.Now().After(deadline) {
			return p.captureLocked(), ErrTimeout
		}
	}
}

// WaitStable blocks until at least quietFor elapses with no new output,
// or the timeout fires. Refuses to declare stability before the first
// feed has been observed (or the pump has closed) — otherwise a call
// made immediately after Run would return on an empty screen if the
// app's first paint is later than quietFor.
func (p *Pump) WaitStable(ctx context.Context, quietFor, timeout time.Duration) error {
	_, err := p.WaitStableCapture(ctx, quietFor, timeout)
	return err
}

// WaitStableCapture is WaitStable with the terminal capture at completion.
func (p *Pump) WaitStableCapture(ctx context.Context, quietFor, timeout time.Duration) (Capture, error) {
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
			return p.captureLocked(), nil
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
			return p.captureLocked(), ErrTimeout
		}
		timer.Reset(wakeIn)
		p.cond.Wait()
		if ctx.Err() != nil {
			return p.captureLocked(), ctx.Err()
		}
		if time.Now().After(deadline) {
			// Final re-check before declaring timeout.
			if p.gotAnyFeed && time.Since(p.lastFeed) >= quietFor {
				return p.captureLocked(), nil
			}
			return p.captureLocked(), ErrTimeout
		}
	}
}

// WaitStableExcept waits for the portion of the screen outside excluded to
// remain unchanged for quietFor. It intentionally compares snapshots rather
// than using lastFeed: output that changes only an excluded region must not
// delay success. A closed pump remains trivially stable, matching WaitStable.
func (p *Pump) WaitStableExcept(ctx context.Context, quietFor, timeout time.Duration, excluded []Rect) error {
	_, err := p.WaitStableExceptCapture(ctx, quietFor, timeout, excluded)
	return err
}

// WaitStableExceptCapture is WaitStableExcept with the terminal capture at completion.
func (p *Pump) WaitStableExceptCapture(ctx context.Context, quietFor, timeout time.Duration, excluded []Rect) (Capture, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	deadline := time.Now().Add(timeout)
	stop := make(chan struct{})
	defer close(stop)

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

	var previous vt.Snapshot
	haveSnapshot := false
	var lastChange time.Time
	for {
		now := time.Now()
		if p.gotAnyFeed && !haveSnapshot {
			previous = p.model.Snapshot()
			haveSnapshot = true
			lastChange = p.lastFeed
		} else if haveSnapshot {
			current := p.model.Snapshot()
			if !sameOutside(previous, current, excluded) {
				lastChange = now
			}
			// Always advance the baseline, including when only excluded cells
			// changed. This keeps comparisons correct across later updates.
			previous = current
		}

		if (haveSnapshot && now.Sub(lastChange) >= quietFor) || p.closed {
			return p.captureLocked(), nil
		}

		var wakeIn time.Duration
		if haveSnapshot {
			wakeIn = quietFor - now.Sub(lastChange)
		} else {
			wakeIn = deadline.Sub(now)
		}
		if remaining := deadline.Sub(now); remaining < wakeIn {
			wakeIn = remaining
		}
		if wakeIn <= 0 {
			return p.captureLocked(), ErrTimeout
		}
		timer.Reset(wakeIn)
		p.cond.Wait()
		if ctx.Err() != nil {
			return p.captureLocked(), ctx.Err()
		}
		if time.Now().After(deadline) {
			// A feed and the deadline can race. Compare one last snapshot so
			// an unexcluded update just before the deadline cannot be mistaken
			// for an already-stable screen.
			if haveSnapshot {
				if !sameOutside(previous, p.model.Snapshot(), excluded) {
					return p.captureLocked(), ErrTimeout
				}
				if time.Since(lastChange) >= quietFor {
					return p.captureLocked(), nil
				}
			}
			return p.captureLocked(), ErrTimeout
		}
	}
}

func sameOutside(a, b vt.Snapshot, excluded []Rect) bool {
	if a.Size != b.Size || a.AltScreen != b.AltScreen || len(a.Lines) != len(b.Lines) {
		return false
	}
	if !excludedCell(a.Cursor.Col, a.Cursor.Row, a.Size, excluded) ||
		!excludedCell(b.Cursor.Col, b.Cursor.Row, b.Size, excluded) {
		if a.Cursor != b.Cursor {
			return false
		}
	}
	for y := range a.Lines {
		if len(a.Lines[y].Cells) != len(b.Lines[y].Cells) {
			return false
		}
		for x := range a.Lines[y].Cells {
			if !excludedCell(x, y, a.Size, excluded) && a.Lines[y].Cells[x] != b.Lines[y].Cells[x] {
				return false
			}
		}
	}
	return true
}

func excludedCell(x, y int, size vt.Size, excluded []Rect) bool {
	if x < 0 || y < 0 || x >= size.Cols || y >= size.Rows {
		return false
	}
	for _, rect := range excluded {
		if rect.X < 0 || rect.Y < 0 || rect.W <= 0 || rect.H <= 0 {
			continue
		}
		// Subtraction avoids overflow when a valid but very large width or
		// height is clipped to the live viewport.
		if x >= rect.X && y >= rect.Y && x-rect.X < rect.W && y-rect.Y < rect.H {
			return true
		}
	}
	return false
}

// Errors.
var (
	ErrTimeout = errors.New("pump: timeout")
	ErrClosed  = errors.New("pump: closed")
)
