package daemon

import (
	"encoding/base64"
	"encoding/json"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/render"
	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	optionalRegistrations = append(optionalRegistrations, func(d *Dispatcher) {
		d.Register(rpc.OpScreenshot, handleScreenshot)
	})
}

func handleScreenshot(t *engine.Term, raw json.RawMessage) (any, *rpc.Error) {
	a, errResp := decodeOptionalArgs[rpc.ScreenshotArgs](raw)
	if errResp != nil {
		return nil, errResp
	}
	snap := t.Snapshot()
	img, err := render.Render(snap, renderOptionsForScreenshot(a))
	if err != nil {
		return nil, internalFailure(err)
	}
	if a.Out != "" {
		if err := render.EncodePNGFile(a.Out, img); err != nil {
			return nil, ioFailure(err)
		}
		return rpc.ScreenshotData{
			Out:    a.Out,
			Width:  img.Bounds().Dx(),
			Height: img.Bounds().Dy(),
		}, nil
	}
	pngBytes, err := render.PNGBytes(img)
	if err != nil {
		return nil, internalFailure(err)
	}
	return rpc.ScreenshotData{
		PNGBase64: base64.StdEncoding.EncodeToString(pngBytes),
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
