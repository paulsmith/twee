// Package render rasterizes a terminal cell-grid Snapshot to a PNG.
package render

import (
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

var (
	faceMu    sync.Mutex
	faceFont  *opentype.Font
	faceCache = map[float64]font.Face{}
)

// Face returns a cached font.Face at the requested size in points.
// Callers must not Close the returned Face.
func Face(sizePt float64) (font.Face, error) {
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[sizePt]; ok {
		return f, nil
	}
	if faceFont == nil {
		f, err := opentype.Parse(jetbrainsMonoRegular)
		if err != nil {
			return nil, err
		}
		faceFont = f
	}
	face, err := opentype.NewFace(faceFont, &opentype.FaceOptions{
		Size:    sizePt,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	faceCache[sizePt] = face
	return face, nil
}
