package networkcapture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/ptyrunner"
	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/third_party/netwrap"
)

func TestStageBuildsRuntimeConfigAndCleansUp(t *testing.T) {
	staging, cfg, err := Stage([]Publication{{
		Listen: "127.0.0.1:8080",
		Guest:  "10.0.2.100:3000",
	}})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(staging.PCAPPath())
	if cfg.PCAPPath != staging.PCAPPath() {
		t.Fatalf("runner PCAP path = %q, want %q", cfg.PCAPPath, staging.PCAPPath())
	}
	wantPublication := netwrap.TCPPublication{
		Listen: "127.0.0.1:8080",
		Guest:  "10.0.2.100:3000",
	}
	if len(cfg.PublishTCP) != 1 || cfg.PublishTCP[0] != wantPublication {
		t.Fatalf("runner publications = %+v, want %+v", cfg.PublishTCP, wantPublication)
	}
	if err := staging.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging directory still exists: %v", err)
	}
}

func TestStageRejectsInvalidPublication(t *testing.T) {
	for _, guest := range []string{"", "10.0.2.100", "10.0.2.100:http", "10.0.2.100:0", "10.0.2.100:65536"} {
		_, _, err := Stage([]Publication{{Listen: "127.0.0.1:8080", Guest: guest}})
		if err == nil || !strings.Contains(err.Error(), "network capture publication metadata") {
			t.Errorf("guest %q error = %v", guest, err)
		}
	}
}

func TestCleanupReturnsRemovalError(t *testing.T) {
	want := errors.New("cannot remove staging")
	staging := &Staging{
		dir: "/synthetic/network-capture",
		removeAll: func(path string) error {
			if path != "/synthetic/network-capture" {
				t.Fatalf("cleanup path = %q", path)
			}
			return want
		},
	}
	if err := staging.Cleanup(); !errors.Is(err, want) {
		t.Fatalf("cleanup error = %v", err)
	}
}

func TestTraceCaptureProjectsCompletedResult(t *testing.T) {
	staging := &Staging{publications: []string{"127.0.0.1:8080=3000"}}
	got, err := staging.TraceCapture(fakeCompletedRunner{result: ptyrunner.NetworkCaptureResult{
		MaxBytes: 1000, BytesWritten: 750, PacketCount: 12, Truncated: true,
	}, captured: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != trace.NetworkCaptureFormat || got.Stream != trace.NetworkCaptureStream || got.GVisorVersion != netwrap.GVisorVersion {
		t.Fatalf("capture identity = %+v", got)
	}
	if len(got.PublishTCP) != 1 || got.PublishTCP[0] != "127.0.0.1:8080=3000" {
		t.Fatalf("publications = %v", got.PublishTCP)
	}
	if got.ByteLimit != 1000 || got.CapturedBytes != 750 || got.PacketCount != 12 || !got.Truncated || got.Status != trace.NetworkCaptureStatusTruncated {
		t.Fatalf("capture result = %+v", got)
	}
}

func TestTraceCaptureReportsCompleteResult(t *testing.T) {
	staging := &Staging{}
	got, err := staging.TraceCapture(fakeCompletedRunner{
		result:   ptyrunner.NetworkCaptureResult{MaxBytes: 1000},
		captured: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Truncated || got.Status != trace.NetworkCaptureStatusComplete {
		t.Fatalf("capture result = %+v", got)
	}
}

func TestTraceCaptureReportsRunnerFailures(t *testing.T) {
	runtimeErr := errors.New("recorder failed")
	staging := &Staging{}
	if _, err := staging.TraceCapture(fakeCompletedRunner{err: runtimeErr}); !errors.Is(err, runtimeErr) || !strings.Contains(err.Error(), "network capture runtime") {
		t.Fatalf("runtime error = %v", err)
	}
	if _, err := staging.TraceCapture(fakeCompletedRunner{}); err == nil || !strings.Contains(err.Error(), "runner did not provide capture results") {
		t.Fatalf("missing result error = %v", err)
	}
}

type fakeCompletedRunner struct {
	err      error
	result   ptyrunner.NetworkCaptureResult
	captured bool
}

func (r fakeCompletedRunner) Err() error { return r.err }

func (r fakeCompletedRunner) NetworkCapture() (ptyrunner.NetworkCaptureResult, bool) {
	return r.result, r.captured
}
