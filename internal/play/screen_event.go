package play

import (
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/vt"
)

// ApplyScreenEvent applies the terminal-model mechanics of ev. It reports
// whether ev is an output event or a resize with positive dimensions; the
// result does not imply that the model's rendered pixels changed.
//
// Input, exit, unknown, and invalid resize events do not affect the model.
func ApplyScreenEvent(model vt.Model, ev Event) (screenEvent bool, err error) {
	switch ev.Type {
	case trace.EventTypeOutput:
		return true, model.Feed(ev.Bytes)
	case trace.EventTypeResize:
		if ev.Cols > 0 && ev.Rows > 0 {
			return true, model.Resize(ev.Cols, ev.Rows)
		}
	}
	return false, nil
}
