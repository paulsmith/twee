// Package networkcapture stages managed packet captures and projects completed
// runner results into trace metadata.
package networkcapture

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/third_party/netwrap"
)

// Publication maps a host listener to a private guest address.
type Publication struct {
	Listen string
	Guest  string
}

// Staging owns the temporary file used by one network capture.
type Staging struct {
	dir          string
	pcapPath     string
	publications []string
	removeAll    func(string) error
}

// Stage creates temporary storage and the runner configuration for a network
// capture. Callers must call Cleanup after the runner and trace are finished.
func Stage(publications []Publication) (*Staging, *ptyrunner.NetworkConfig, error) {
	runtimePublications := make([]netwrap.TCPPublication, len(publications))
	manifestPublications := make([]string, len(publications))
	for i, publication := range publications {
		runtimePublications[i] = netwrap.TCPPublication{
			Listen: publication.Listen,
			Guest:  publication.Guest,
		}
		var err error
		manifestPublications[i], err = formatPublication(publication)
		if err != nil {
			return nil, nil, fmt.Errorf("network capture publication metadata: %w", err)
		}
	}

	dir, err := os.MkdirTemp("", "twee-network-*")
	if err != nil {
		return nil, nil, fmt.Errorf("network capture staging: %w", err)
	}
	staging := &Staging{
		dir:          dir,
		pcapPath:     filepath.Join(dir, "network.pcap"),
		publications: manifestPublications,
		removeAll:    os.RemoveAll,
	}
	return staging, &ptyrunner.NetworkConfig{
		PCAPPath:   staging.pcapPath,
		PublishTCP: runtimePublications,
	}, nil
}

// formatPublication returns the public trace representation of publication.
// The private guest address is a runtime implementation detail; only its port
// belongs in public artifacts.
func formatPublication(publication Publication) (string, error) {
	_, portText, err := net.SplitHostPort(publication.Guest)
	if err != nil {
		return "", fmt.Errorf("guest address %q: %w", publication.Guest, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("guest address %q: port must be a number from 1 through 65535", publication.Guest)
	}
	return publication.Listen + "=" + strconv.Itoa(port), nil
}

// PCAPPath returns the path where the runner writes captured packets.
func (s *Staging) PCAPPath() string { return s.pcapPath }

// Cleanup removes the capture's temporary staging directory.
func (s *Staging) Cleanup() error { return s.removeAll(s.dir) }

// CompletedRunner exposes stable state from a runner whose lifecycle has
// completed.
type CompletedRunner interface {
	Err() error
	NetworkCapture() (ptyrunner.NetworkCaptureResult, bool)
}

// TraceCapture projects a completed runner's capture into trace metadata.
func (s *Staging) TraceCapture(runner CompletedRunner) (trace.NetworkCapture, error) {
	if err := runner.Err(); err != nil {
		return trace.NetworkCapture{}, fmt.Errorf("network capture runtime: %w", err)
	}
	result, ok := runner.NetworkCapture()
	if !ok {
		return trace.NetworkCapture{}, errors.New("network capture: runner did not provide capture results")
	}
	status := trace.NetworkCaptureStatusComplete
	if result.Truncated {
		status = trace.NetworkCaptureStatusTruncated
	}
	return trace.NetworkCapture{
		Format:        trace.NetworkCaptureFormat,
		Stream:        trace.NetworkCaptureStream,
		GVisorVersion: netwrap.GVisorVersion,
		PublishTCP:    append([]string(nil), s.publications...),
		ByteLimit:     result.MaxBytes,
		CapturedBytes: result.BytesWritten,
		PacketCount:   int64(result.PacketCount),
		Truncated:     result.Truncated,
		Status:        status,
	}, nil
}
