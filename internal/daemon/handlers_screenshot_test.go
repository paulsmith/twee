package daemon

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func TestScreenshotUsesRequestedPixelSize(t *testing.T) {
	te := startTestTerm(t)
	out := filepath.Join(t.TempDir(), "screen.png")
	raw, err := json.Marshal(rpc.ScreenshotArgs{
		Out:         out,
		PixelWidth:  123,
		PixelHeight: 45,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	data, rpcErr := handleScreenshot(te, raw)
	if rpcErr != nil {
		t.Fatalf("handleScreenshot: %+v", rpcErr)
	}
	got, ok := data.(rpc.ScreenshotData)
	if !ok {
		t.Fatalf("data type = %T, want rpc.ScreenshotData", data)
	}
	if got.Width != 123 || got.Height != 45 {
		t.Fatalf("response size = %dx%d, want 123x45", got.Width, got.Height)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open screenshot: %v", err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}
	if cfg.Width != 123 || cfg.Height != 45 {
		t.Fatalf("png size = %dx%d, want 123x45", cfg.Width, cfg.Height)
	}
}
