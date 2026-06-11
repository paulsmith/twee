package engine

import (
	"fmt"
	"os"

	"github.com/paulsmith/twee/internal/input"
)

// Type writes literal text to the PTY.
func (t *Term) Type(s string) error {
	if _, err := t.runner.Master().Write([]byte(s)); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Type %q", s))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput("type", "", []byte(s))
	}
	return nil
}

// Key sends a named key.
func (t *Term) Key(k input.Key) error {
	b := input.Encode(k)
	if len(b) == 0 {
		return nil
	}
	if _, err := t.runner.Master().Write(b); err != nil {
		return err
	}
	t.recordInput("Key " + input.Name(k))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput("key", input.Name(k), b)
	}
	return nil
}

// Paste sends text wrapped in bracketed-paste markers.
func (t *Term) Paste(text string) error {
	b := input.EncodePaste(text)
	if _, err := t.runner.Master().Write(b); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Paste %q", text))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput("paste", "", b)
	}
	return nil
}

// Resize updates the PTY winsize, signals the child with SIGWINCH, and
// resizes the model.
func (t *Term) Resize(cols, rows int) error {
	if err := t.runner.Resize(cols, rows); err != nil {
		return err
	}
	if err := t.pump.Resize(cols, rows); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Resize %dx%d", cols, rows))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteResize(cols, rows)
	}
	return nil
}

// Signal forwards a signal to the child process.
func (t *Term) Signal(sig os.Signal) error {
	t.recordInput(fmt.Sprintf("Signal %v", sig))
	return t.runner.Signal(sig)
}
