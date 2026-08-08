package codegen

import (
	"errors"
	"strings"
	"testing"
)

func TestNetworkCaptureCleanupErrorIsJoined(t *testing.T) {
	primary := errors.New("primary failure")
	cleanup := errors.New("cleanup failure")
	artifacts := &networkCaptureArtifacts{
		dir: "/synthetic/network-capture",
		removeAll: func(path string) error {
			if path != "/synthetic/network-capture" {
				t.Fatalf("cleanup path = %q", path)
			}
			return cleanup
		},
	}
	err := artifacts.joinCleanupError(primary)
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) || !strings.Contains(err.Error(), "network capture cleanup") {
		t.Fatalf("joined error = %v", err)
	}
}
