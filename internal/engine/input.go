package engine

import (
	"fmt"
	"io"
	"os"

	"github.com/paulsmith/twee/internal/input"
	"github.com/paulsmith/twee/internal/trace"
)

// Type writes literal text to the PTY.
func (t *Term) Type(s string) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	if err := writeAll(t.inputWriter, []byte(s)); err != nil {
		return err
	}
	t.recordInput(fmt.Sprintf("Type %q", s))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput(trace.InputKindType, "", []byte(s))
	}
	return nil
}

// Key sends a named key.
func (t *Term) Key(k input.Key) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	b, err := t.pump.EncodeKey(k)
	if err != nil {
		return failedPrecondition(err.Error(), map[string]any{
			"key": input.Name(k),
		}, err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := writeAll(t.inputWriter, b); err != nil {
		return inputIO(err)
	}
	t.recordInput("Key " + input.Name(k))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput(trace.InputKindKey, input.Name(k), b)
	}
	return nil
}

// Paste sends text wrapped in bracketed-paste markers when the child has
// enabled DEC mode 2004. Use ForcePaste only when literal marker delivery is
// deliberately desired despite a known-disabled mode.
func (t *Term) Paste(text string) error {
	return t.paste(text, false)
}

// ForcePaste sends bracketed-paste markers even when DEC mode 2004 is known
// to be disabled. It is the explicit escape hatch for raw protocol testing.
func (t *Term) ForcePaste(text string) error {
	return t.paste(text, true)
}

func (t *Term) paste(text string, force bool) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

	if !force {
		presentation, err := t.pump.Presentation()
		if err != nil {
			return failedPrecondition(err.Error(), map[string]any{
				"operation": "paste",
			}, err)
		}
		if !presentation.Input.BracketedPaste {
			return failedPrecondition(
				"bracketed paste mode is disabled",
				map[string]any{"mode": 2004, "force_available": true},
				nil,
			)
		}
	}
	b := input.EncodePaste(text)
	if err := writeAll(t.inputWriter, b); err != nil {
		return inputIO(err)
	}
	t.recordInput(fmt.Sprintf("Paste %q", text))
	t.cfgMu.Lock()
	tr := t.tr
	t.cfgMu.Unlock()
	if tr != nil {
		tr.WriteInput(trace.InputKindPaste, "", b)
	}
	return nil
}

// Resize updates the PTY winsize, signals the child with SIGWINCH, and
// resizes the model.
func (t *Term) Resize(cols, rows int) error {
	t.inputMu.Lock()
	defer t.inputMu.Unlock()

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

// writeAll writes one complete logical input. Writers are allowed to return a
// short count, so loop until the full buffer has been consumed. A writer that
// makes no progress without returning an error is treated as a short write.
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return fmt.Errorf("invalid write count %d for buffer length %d", n, len(b))
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// Signal forwards a signal to the child process.
func (t *Term) Signal(sig os.Signal) error {
	t.recordInput(fmt.Sprintf("Signal %v", sig))
	return t.runner.Signal(sig)
}
