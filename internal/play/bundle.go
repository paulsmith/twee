// Package play implements trace bundle playback for the twee CLI.
package play

import (
	"fmt"

	"github.com/paulsmith/twee/internal/tracebundle"
)

// Bundle is the decoded contents needed for playback.
type Bundle = tracebundle.Bundle

// Event is one decoded events.jsonl record.
type Event = tracebundle.Event

// OpenBundle opens and decodes a .twee zip bundle.
func OpenBundle(path string) (Bundle, error) {
	bundle, err := tracebundle.Open(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("twee play: %w", err)
	}
	return bundle, nil
}
