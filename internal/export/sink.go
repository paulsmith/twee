package export

import (
	"image"
	"time"
)

// sink consumes composed frames and writes the output file on close.
type sink interface {
	add(img *image.RGBA, d time.Duration) error
	close() error
}
