package codegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/third_party/netwrap"
)

type networkCaptureArtifacts struct {
	dir          string
	pcapPath     string
	publications []string
	removeAll    func(string) error
}

func stageNetworkCapture(opts Options) (*networkCaptureArtifacts, *ptyrunner.NetworkConfig, error) {
	if !opts.NetworkCapture {
		return nil, nil, nil
	}
	publications := make([]netwrap.TCPPublication, len(opts.PublishTCP))
	manifestPublications := make([]string, len(opts.PublishTCP))
	for i, publication := range opts.PublishTCP {
		publications[i] = netwrap.TCPPublication{Listen: publication.Listen, Guest: publication.Guest}
		var err error
		manifestPublications[i], err = engine.FormatTCPPublication(publication)
		if err != nil {
			return nil, nil, fmt.Errorf("network capture publication metadata: %w", err)
		}
	}
	dir, err := os.MkdirTemp("", "twee-network-*")
	if err != nil {
		return nil, nil, fmt.Errorf("network capture staging: %w", err)
	}
	artifacts := &networkCaptureArtifacts{
		dir:          dir,
		pcapPath:     filepath.Join(dir, "network.pcap"),
		publications: manifestPublications,
		removeAll:    os.RemoveAll,
	}
	return artifacts, &ptyrunner.NetworkConfig{
		PCAPPath: artifacts.pcapPath, PublishTCP: publications,
	}, nil
}

func (a *networkCaptureArtifacts) cleanup() error {
	return a.removeAll(a.dir)
}

func (a *networkCaptureArtifacts) joinCleanupError(err error) error {
	if cleanupErr := a.cleanup(); cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("network capture cleanup: %w", cleanupErr))
	}
	return err
}

func (a *networkCaptureArtifacts) finalize(traces *traceController, runner *ptyrunner.Runner) error {
	abort := func(err error) error {
		return traces.abort(err)
	}
	if err := runner.Err(); err != nil {
		return abort(fmt.Errorf("network capture runtime: %w", err))
	}
	result, ok := runner.NetworkCapture()
	if !ok {
		return abort(errors.New("network capture: runner did not provide capture results"))
	}
	status := trace.NetworkCaptureStatusComplete
	if result.Truncated {
		status = trace.NetworkCaptureStatusTruncated
	}
	err := traces.attachNetworkCapture(a.pcapPath, trace.NetworkCapture{
		Format:        trace.NetworkCaptureFormat,
		Stream:        trace.NetworkCaptureStream,
		GVisorVersion: netwrap.GVisorVersion,
		PublishTCP:    a.publications,
		ByteLimit:     result.MaxBytes,
		CapturedBytes: result.BytesWritten,
		PacketCount:   int64(result.PacketCount),
		Truncated:     result.Truncated,
		Status:        status,
	})
	if err != nil {
		return abort(err)
	}
	return nil
}
