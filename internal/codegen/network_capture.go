package codegen

import (
	"errors"
	"fmt"

	"github.com/paulsmith/twee/internal/networkcapture"
	"github.com/paulsmith/twee/internal/ptyrunner"
)

func stageNetworkCapture(opts Options) (*networkcapture.Staging, *ptyrunner.NetworkConfig, error) {
	if !opts.NetworkCapture {
		return nil, nil, nil
	}
	return networkcapture.Stage(opts.PublishTCP)
}

func joinNetworkCaptureCleanupError(err error, cleanup func() error) error {
	if cleanupErr := cleanup(); cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("network capture cleanup: %w", cleanupErr))
	}
	return err
}

func finalizeNetworkCapture(capture *networkcapture.Staging, traces *traceController, runner *ptyrunner.Runner) error {
	abort := func(err error) error {
		return traces.abort(err)
	}
	metadata, err := capture.TraceCapture(runner)
	if err != nil {
		return abort(err)
	}
	err = traces.attachNetworkCapture(capture.PCAPPath(), metadata)
	if err != nil {
		return abort(err)
	}
	return nil
}
