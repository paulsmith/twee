package codegen

import (
	"errors"
	"strings"
	"testing"
)

func TestNetworkCaptureCleanupErrorIsJoined(t *testing.T) {
	primary := errors.New("primary failure")
	cleanup := errors.New("cleanup failure")
	err := joinNetworkCaptureCleanupError(primary, func() error { return cleanup })
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) || !strings.Contains(err.Error(), "network capture cleanup") {
		t.Fatalf("joined error = %v", err)
	}
}
