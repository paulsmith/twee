package codegen

import (
	"fmt"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

// scriptController owns the single permitted script artifact for a wrap run.
// It deliberately has no paused state: stopping is durable finalization.
type scriptController struct {
	state       recorderState
	path        string
	partial     bool
	rec         *recorder
	reservation *pathReservation
}

func (c *scriptController) start(path string, cols, rows int, partial bool) error {
	if c.state != recorderIdle {
		return fmt.Errorf("script already finalized: %s", terminalPath(c.path))
	}
	generated := path == ""
	var reservation *pathReservation
	if generated {
		var err error
		path, reservation, err = reserveRecorderPath("twee-script", ".json", time.Now())
		if err != nil {
			c.state = recorderFailed
			return err
		}
	}
	c.reservation = reservation
	c.path, c.partial, c.rec, c.state = path, partial, &recorder{}, recorderRecording
	if err := c.rec.Resize(cols, rows); err != nil {
		cleanupReservedPath(c.path, c.reservation)
		c.state = recorderFailed
		return err
	}
	return nil
}

func (c *scriptController) close() error {
	if c.state != recorderRecording {
		return nil
	}
	err := writeScript(c.path, c.rec.Requests())
	if err != nil {
		cleanupReservedPath(c.path, c.reservation)
		c.state = recorderFailed
		return err
	}
	releaseReservation(c.reservation)
	c.state = recorderFinalized
	return nil
}

func (c *scriptController) requests() []rpc.Request {
	if c.rec == nil {
		return nil
	}
	return c.rec.Requests()
}
