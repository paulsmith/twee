package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/render"
	"github.com/paulsmith/research/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpScreenshot, handleScreenshot)
	})
}

func handleScreenshot(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	var a rpc.ScreenshotArgs
	if err := json.Unmarshal(raw, &a); err != nil && len(raw) > 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidArgument, Message: err.Error()}
	}
	snap := t.Snapshot()
	img, err := render.Render(snap, renderOptionsForScreenshot(a))
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
	}
	if a.Out != "" {
		f, err := os.Create(a.Out)
		if err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		defer f.Close()
		if err := render.EncodePNG(f, img); err != nil {
			return nil, &rpc.Error{Code: rpc.CodeIO, Message: err.Error()}
		}
		return rpc.ScreenshotData{
			Out:    a.Out,
			Width:  img.Bounds().Dx(),
			Height: img.Bounds().Dy(),
		}, nil
	}
	var buf bytes.Buffer
	if err := render.EncodePNG(&buf, img); err != nil {
		return nil, &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
	}
	return rpc.ScreenshotData{
		PNGBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:     img.Bounds().Dx(),
		Height:    img.Bounds().Dy(),
	}, nil
}

func renderOptionsForScreenshot(a rpc.ScreenshotArgs) render.Options {
	if a.PixelWidth > 0 && a.PixelHeight > 0 {
		return render.Options{PixelWidth: a.PixelWidth, PixelHeight: a.PixelHeight}
	}
	return render.Default()
}
