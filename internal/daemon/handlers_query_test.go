package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/engine"
	"github.com/paulsmith/twee/internal/rpc"
)

func TestQueryHandlers(t *testing.T) {
	te := startTestTerm(t)

	textData, rpcErr := handleText(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleText: %+v", rpcErr)
	}
	if got := textData.(rpc.TextData).Text; !strings.Contains(got, "hello") {
		t.Fatalf("text = %q, want hello", got)
	}

	linesData, rpcErr := handleLines(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleLines: %+v", rpcErr)
	}
	lines := linesData.(rpc.LinesData).Lines
	if len(lines) == 0 || !strings.Contains(lines[0], "hello") {
		t.Fatalf("lines = %#v, want first line with hello", lines)
	}

	cellResult, rpcErr := handleCell(te, mustJSON(t, rpc.CellArgs{X: 0, Y: 0}))
	if rpcErr != nil {
		t.Fatalf("handleCell: %+v", rpcErr)
	}
	cell := cellResult.(rpc.CellData)
	if cell.Text != "h" {
		t.Fatalf("cell text = %q, want h", cell.Text)
	}
	if cell.Fg.Kind != rpc.ColorKindDefault {
		t.Fatalf("cell fg kind = %q, want %q", cell.Fg.Kind, rpc.ColorKindDefault)
	}

	regionData, rpcErr := handleRegion(te, mustJSON(t, rpc.RegionArgs{X: 0, Y: 0, W: 5, H: 1}))
	if rpcErr != nil {
		t.Fatalf("handleRegion: %+v", rpcErr)
	}
	region := regionData.([][]rpc.CellData)
	if got := cellsText(region[0]); got != "hello" {
		t.Fatalf("region text = %q, want hello", got)
	}

	cursorData, rpcErr := handleCursor(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleCursor: %+v", rpcErr)
	}
	if !cursorData.(rpc.CursorData).Visible {
		t.Fatalf("cursor visible = false, want true")
	}

	sizeData, rpcErr := handleSize(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleSize: %+v", rpcErr)
	}
	if got := sizeData.(rpc.SizeData); got.Cols != 40 || got.Rows != 5 {
		t.Fatalf("size = %dx%d, want 40x5", got.Cols, got.Rows)
	}

	titleData, rpcErr := handleTitle(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleTitle: %+v", rpcErr)
	}
	if got := titleData.(rpc.TitleData).Title; got != "" {
		t.Fatalf("title = %q, want empty", got)
	}

	modeData, rpcErr := handleMode(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleMode: %+v", rpcErr)
	}
	if modeData.(rpc.ModeData).AltScreen {
		t.Fatalf("alt screen = true, want false")
	}

	scrollbackData, rpcErr := handleScrollback(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleScrollback: %+v", rpcErr)
	}
	if got := scrollbackData.(struct {
		Lines []string `json:"lines"`
	}).Lines; len(got) != 0 {
		t.Fatalf("scrollback lines = %#v, want empty", got)
	}

	snapshotData, rpcErr := handleSnapshot(te, nil)
	if rpcErr != nil {
		t.Fatalf("handleSnapshot: %+v", rpcErr)
	}
	if got := snapshotData.(engine.Snapshot); got.Cols != 40 || got.Rows != 5 {
		t.Fatalf("snapshot size = %dx%d, want 40x5", got.Cols, got.Rows)
	}
}

// TestCellRegionWireShape pins down the snake_case wire shape for "cell"
// and "region": every style attribute (including italic and
// strikethrough, which the old engine.Cell-shaped response silently
// dropped) flows through, and a color's "kind" is a string enum with
// palette index 0 distinguishable from "no index" (index is a pointer,
// so it's present-and-zero on the wire, not omitted).
func TestCellRegionWireShape(t *testing.T) {
	// One line: plain, then bold+italic+underline+inverse+strikethrough,
	// then dim, then palette index 0 (the zero-is-not-absent edge case),
	// then 24-bit RGB.
	script := "printf 'P" +
		"\x1b[1;3;4;7;9mB\x1b[0m" +
		"\x1b[2mD\x1b[0m" +
		"\x1b[38;5;0mZ\x1b[0m" +
		"\x1b[38;2;255;0;0mR\x1b[0m" +
		"\\r\\n'; sleep 30"
	te, err := engine.Start(context.Background(), engine.Config{
		Cmd:  []string{"/bin/sh", "-c", script},
		Cols: 40, Rows: 5,
	})
	if err != nil {
		t.Fatalf("engine.Start: %v", err)
	}
	t.Cleanup(func() { _ = te.Close() })
	if err := te.WaitForText("R"); err != nil {
		t.Fatalf("WaitForText: %v", err)
	}

	data, rpcErr := handleRegion(te, mustJSON(t, rpc.RegionArgs{X: 0, Y: 0, W: 5, H: 1}))
	if rpcErr != nil {
		t.Fatalf("handleRegion: %+v", rpcErr)
	}
	row := data.([][]rpc.CellData)[0]

	plain := row[0]
	if plain.Text != "P" {
		t.Fatalf("plain text = %q, want P", plain.Text)
	}
	if plain.Bold || plain.Dim || plain.Italic || plain.Underline || plain.Inverse || plain.Strikethrough {
		t.Fatalf("plain cell has unexpected style set: %+v", plain)
	}
	if plain.Fg.Kind != rpc.ColorKindDefault || plain.Fg.Index != nil {
		t.Fatalf("plain fg = %+v, want kind=default, index=nil", plain.Fg)
	}

	styled := row[1]
	if styled.Text != "B" {
		t.Fatalf("styled text = %q, want B", styled.Text)
	}
	if !styled.Bold || !styled.Italic || !styled.Underline || !styled.Inverse || !styled.Strikethrough {
		t.Fatalf("styled cell missing an expected attribute: %+v", styled)
	}

	dim := row[2]
	if !dim.Dim {
		t.Fatalf("dim cell missing Dim: %+v", dim)
	}

	// handleCell must agree with handleRegion for the same coordinate.
	cellResult, rpcErr := handleCell(te, mustJSON(t, rpc.CellArgs{X: 3, Y: 0}))
	if rpcErr != nil {
		t.Fatalf("handleCell: %+v", rpcErr)
	}
	palZero := cellResult.(rpc.CellData)
	if palZero.Fg.Kind != rpc.ColorKindPalette {
		t.Fatalf("palette-0 fg kind = %q, want %q", palZero.Fg.Kind, rpc.ColorKindPalette)
	}
	if palZero.Fg.Index == nil {
		t.Fatal("palette-0 fg index is nil, want a present pointer to 0")
	}
	if *palZero.Fg.Index != 0 {
		t.Fatalf("palette-0 fg index = %d, want 0", *palZero.Fg.Index)
	}
	// The whole point of using a pointer: index 0 must actually appear on
	// the wire, not be omitted as a JSON zero value.
	raw, err := json.Marshal(palZero)
	if err != nil {
		t.Fatalf("marshal palette-0 cell: %v", err)
	}
	if !strings.Contains(string(raw), `"index":0`) {
		t.Fatalf("marshaled palette-0 cell missing explicit index 0: %s", raw)
	}

	rgb := row[4]
	if rgb.Fg.Kind != rpc.ColorKindRGB {
		t.Fatalf("rgb fg kind = %q, want %q", rgb.Fg.Kind, rpc.ColorKindRGB)
	}
	if rgb.Fg.R != 255 || rgb.Fg.G != 0 || rgb.Fg.B != 0 {
		t.Fatalf("rgb fg = %+v, want {255,0,0}", rgb.Fg)
	}
}

func TestFindHandlerLiteralAndRegex(t *testing.T) {
	te := startTestTerm(t)

	for _, tt := range []struct {
		name string
		args rpc.FindArgs
		want string
	}{
		{"literal", rpc.FindArgs{Text: "ell"}, "ell"},
		{"regex", rpc.FindArgs{Text: `h.llo`, Regex: true}, "hello"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data, rpcErr := handleFind(te, mustJSON(t, tt.args))
			if rpcErr != nil {
				t.Fatalf("handleFind: %+v", rpcErr)
			}
			matches := data.([]rpc.FindMatch)
			if len(matches) != 1 {
				t.Fatalf("matches = %#v, want 1 match", matches)
			}
			if matches[0].Text != tt.want {
				t.Fatalf("match text = %q, want %q", matches[0].Text, tt.want)
			}
		})
	}
}

func TestQueryHandlersRejectInvalidArgs(t *testing.T) {
	te := startTestTerm(t)

	tests := []struct {
		name string
		fn   Handler
		raw  json.RawMessage
	}{
		{"cell json", handleCell, json.RawMessage(`{`)},
		{"cell x", handleCell, mustJSON(t, rpc.CellArgs{X: -1, Y: 0})},
		{"cell y", handleCell, mustJSON(t, rpc.CellArgs{X: 0, Y: -1})},
		{"region json", handleRegion, json.RawMessage(`{`)},
		{"region size", handleRegion, mustJSON(t, rpc.RegionArgs{X: 0, Y: 0, W: 0, H: 1})},
		{"find json", handleFind, json.RawMessage(`{`)},
		{"find regex", handleFind, mustJSON(t, rpc.FindArgs{Text: `(`, Regex: true})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.fn(te, tt.raw); err == nil {
				t.Fatalf("%s unexpectedly succeeded", tt.name)
			} else if err.Code != rpc.CodeInvalidArgument {
				t.Fatalf("error code = %q, want %q", err.Code, rpc.CodeInvalidArgument)
			}
		})
	}
}

func TestRegionAllowsOutOfRangeCoordinates(t *testing.T) {
	te := startTestTerm(t)

	data, rpcErr := handleRegion(te, mustJSON(t, rpc.RegionArgs{X: -3, Y: 0, W: 2, H: 1}))
	if rpcErr != nil {
		t.Fatalf("handleRegion: %+v", rpcErr)
	}
	region := data.([][]rpc.CellData)
	if len(region) != 1 || len(region[0]) != 0 {
		t.Fatalf("region = %#v, want one empty row", region)
	}
}

func cellsText(cells []rpc.CellData) string {
	var b strings.Builder
	for _, c := range cells {
		b.WriteString(c.Text)
	}
	return b.String()
}
