package export

import (
	"image"
	"os"
	"time"
)

// sink consumes composed frames and atomically commits the output file on
// close. abort discards an output that has not been committed yet.
type sink interface {
	add(img *image.RGBA, d time.Duration) error
	close() error
	abort()
}

// preserveDestinationMode applies an existing destination's permissions to a
// completed temporary artifact. New destinations retain CreateTemp's private
// 0600 mode, which cannot be broader than the caller's umask policy.
func preserveDestinationMode(tempPath, outPath string) error {
	info, err := os.Stat(outPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Chmod(tempPath, info.Mode().Perm())
}
