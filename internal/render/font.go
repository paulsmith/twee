// Package render rasterizes a terminal cell-grid Snapshot to a PNG.
package render

import (
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

var (
	faceMu            sync.Mutex
	faceFont          *opentype.Font
	fallbackFaceFont  *opentype.Font
	symbolsFaceFont   *opentype.Font
	symbols2FaceFont  *opentype.Font
	faceCache         = map[float64]font.Face{}
	fallbackFaceCache = map[float64]font.Face{}
	symbolsFaceCache  = map[float64]font.Face{}
	symbols2FaceCache = map[float64]font.Face{}
)

// Face returns a cached font.Face at the requested size in points.
// Callers must not Close the returned Face.
func Face(sizePt float64) (font.Face, error) {
	faceMu.Lock()
	defer faceMu.Unlock()
	return cachedFace(sizePt, &faceFont, jetbrainsMonoRegular, faceCache)
}

// FallbackFace returns a cached Noto Sans Mono face at the requested size in
// points. Callers must not Close the returned Face.
func FallbackFace(sizePt float64) (font.Face, error) {
	faceMu.Lock()
	defer faceMu.Unlock()
	return cachedFace(sizePt, &fallbackFaceFont, notoSansMonoRegular, fallbackFaceCache)
}

// SymbolsFallbackFace returns a cached Noto Sans Symbols face at the
// requested size in points. Callers must not Close the returned Face.
func SymbolsFallbackFace(sizePt float64) (font.Face, error) {
	faceMu.Lock()
	defer faceMu.Unlock()
	return cachedFace(sizePt, &symbolsFaceFont, notoSansSymbolsRegular, symbolsFaceCache)
}

// Symbols2FallbackFace returns a cached Noto Sans Symbols 2 face at the
// requested size in points. Callers must not Close the returned Face.
func Symbols2FallbackFace(sizePt float64) (font.Face, error) {
	faceMu.Lock()
	defer faceMu.Unlock()
	return cachedFace(sizePt, &symbols2FaceFont, notoSansSymbols2Regular, symbols2FaceCache)
}

// FallbackFaces returns fallback faces in preference order.
func FallbackFaces(sizePt float64) ([]font.Face, error) {
	monoFace, err := FallbackFace(sizePt)
	if err != nil {
		return nil, err
	}
	symbolsFace, err := SymbolsFallbackFace(sizePt)
	if err != nil {
		return nil, err
	}
	symbols2Face, err := Symbols2FallbackFace(sizePt)
	if err != nil {
		return nil, err
	}
	return []font.Face{monoFace, symbolsFace, symbols2Face}, nil
}

func cachedFace(sizePt float64, parsed **opentype.Font, data []byte, cache map[float64]font.Face) (font.Face, error) {
	if f, ok := cache[sizePt]; ok {
		return f, nil
	}
	if *parsed == nil {
		f, err := opentype.Parse(data)
		if err != nil {
			return nil, err
		}
		*parsed = f
	}
	face, err := opentype.NewFace(*parsed, &opentype.FaceOptions{
		Size:    sizePt,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	cache[sizePt] = face
	return face, nil
}
