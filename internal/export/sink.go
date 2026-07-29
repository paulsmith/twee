package export

import (
	"image"
	"time"
)

// sink consumes composed frames and atomically commits the output file on
// close. abort discards an output that has not been committed yet.
type sink interface {
	add(img *image.RGBA, d time.Duration) error
	close() error
	abort()
}
