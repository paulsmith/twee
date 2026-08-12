package export

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBundle(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.twee")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	mw, err := zw.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprint(mw, `{"version":1,"command":["super-secret-command"],"env":{"TWEE_SECRET":"secret-environment-value"},"cols":20,"rows":5,"pid":424242,"host":{"os":"secret-os","arch":"secret-arch","hostname":"secret-hostname"}}`)
	ew, err := zw.Create("events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	_, _ = fmt.Fprintf(ew, `{"t_ms":100,"type":"output","bytes_b64":"%s"}`+"\n", b64("hello"))
	_, _ = fmt.Fprintf(ew, `{"t_ms":300,"type":"input","bytes_b64":"%s"}`+"\n", b64("secret-input-bytes"))
	_, _ = fmt.Fprintf(ew, `{"t_ms":600,"type":"output","bytes_b64":"%s"}`+"\n", b64("\r\nworld"))
	_, _ = fmt.Fprint(ew, `{"t_ms":1000,"type":"exit","code":0}`+"\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExportGIFEndToEnd(t *testing.T) {
	bundle := writeTestBundle(t)
	out := filepath.Join(t.TempDir(), "out.gif")
	if err := Export(bundle, out, Options{}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) < 2 {
		t.Errorf("got %d frames, want >= 2 (two distinct screens)", len(g.Image))
	}
	if g.Image[0].Bounds().Dx()%2 != 0 || g.Image[0].Bounds().Dy()%2 != 0 {
		t.Errorf("frame dims %v not even", g.Image[0].Bounds())
	}
}

func TestExportUnknownExtension(t *testing.T) {
	bundle := writeTestBundle(t)
	err := Export(bundle, filepath.Join(t.TempDir(), "out.avi"), Options{})
	if err == nil {
		t.Fatal("want error for unsupported extension")
	}
}

func TestExportMP4RequiresFFmpeg(t *testing.T) {
	bundle := writeTestBundle(t)
	err := Export(bundle, filepath.Join(t.TempDir(), "out.mp4"),
		Options{FFmpeg: "/nonexistent/ffmpeg"})
	if err == nil {
		t.Fatal("want preflight error when ffmpeg missing")
	}
}
