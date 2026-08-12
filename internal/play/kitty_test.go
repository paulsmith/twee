package play

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestWriteKittyDataSingleChunk(t *testing.T) {
	var buf bytes.Buffer
	if err := writeKittyData(&buf, []byte("abc"), kittyPlacement{
		imageID: 7, placementID: 1, cols: 10, rows: 3, chunkSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	want := "\x1b_Ga=T,i=7,p=1,t=d,f=100,c=10,r=3,q=2,C=1,m=0;YWJj\x1b\\"
	if got := buf.String(); got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestWriteKittyDataMultiChunk(t *testing.T) {
	var buf bytes.Buffer
	if err := writeKittyData(&buf, []byte("abcdef"), kittyPlacement{
		imageID: 7, placementID: 1, cols: 10, rows: 3, chunkSize: 4,
	}); err != nil {
		t.Fatal(err)
	}
	want := "\x1b_Ga=T,i=7,p=1,t=d,f=100,c=10,r=3,q=2,C=1,m=1;YWJj\x1b\\\x1b_Gq=2,m=0;ZGVm\x1b\\"
	if got := buf.String(); got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestKittySinkWritesPinnedStatusRow(t *testing.T) {
	var buf bytes.Buffer
	sink := &kittySink{w: &buf, imageID: 9, placementID: 1, chunkSize: 1000, terminalCols: 80, terminalRows: 26}
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 0, color.RGBA{0, 0, 255, 255})

	if err := sink.Emit(img, 2, 24, "toast", "status", true); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"\x1b[H",
		"\x1b_Ga=T,i=9,p=1,t=d,f=100,c=2,r=24,q=2,C=1",
		"\x1b[26;1H\x1b[0m\x1b[2K\x1b[7mstatus │ toast",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q in %q", want, got)
		}
	}
}

func TestKittySinkClearsStatusWhenHidden(t *testing.T) {
	var buf bytes.Buffer
	sink := &kittySink{w: &buf, imageID: 9, placementID: 1, chunkSize: 1000, terminalCols: 80, terminalRows: 6}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	if err := sink.Emit(img, 1, 5, "", "visible", true); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(img, 1, 6, "", "hidden", false); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "\x1b[6;1H\x1b[0m\x1b[2K\x1b[H") {
		t.Fatalf("output did not clear old status row: %q", got)
	}
}

func TestKittySinkReusesStableImageID(t *testing.T) {
	var buf bytes.Buffer
	sink := &kittySink{w: &buf, imageID: 9, placementID: 1, chunkSize: 1000}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	if err := sink.Emit(img, 1, 3, "", "first", true); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(img, 1, 3, "", "second", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), "i=9"); got != 2 {
		t.Fatalf("image id occurrences = %d, want 2", got)
	}
}

func TestKittySinkClearsPreviousImageAndKeepsStatusPinned(t *testing.T) {
	var buf bytes.Buffer
	sink := &kittySink{w: &buf, imageID: 9, placementID: 1, chunkSize: 1000, terminalCols: 80, terminalRows: 6}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	if err := sink.Emit(img, 1, 4, "old toast", "old status", true); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(img, 1, 2, "new toast", "new status", true); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"\x1b[3;1H\x1b[0J\x1b[H",
		"\x1b[6;1H\x1b[0m\x1b[2K\x1b[7mnew status │ new toast",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q in %q", want, got)
		}
	}
}

func TestKittySinkSanitizesAndTruncatesStatusRow(t *testing.T) {
	var buf bytes.Buffer
	sink := &kittySink{w: &buf, imageID: 9, placementID: 1, chunkSize: 1000, terminalCols: 12, terminalRows: 4}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	if err := sink.Emit(img, 1, 3, "abc\x1b[2Jdef", "123456789012345", true); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("footer contains raw escape: %q", got)
	}
	if !strings.Contains(got, "12345678901\u2026") {
		t.Fatalf("truncated status missing, got %q", got)
	}
}
