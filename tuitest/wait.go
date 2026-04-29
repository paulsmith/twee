package tuitest

import (
	"context"
	"time"

	"github.com/paulsmith/research/twee/internal/engine"
)

// WaitOption is re-exported from engine.
type WaitOption = engine.WaitOption

// WithTimeout overrides the timeout for one wait call.
func WithTimeout(d time.Duration) WaitOption { return engine.WithTimeout(d) }

// WithContext lets the wait be canceled externally.
func WithContext(ctx context.Context) WaitOption { return engine.WithContext(ctx) }
