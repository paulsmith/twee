package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/paulsmith/research/twee/internal/engine"
	"github.com/paulsmith/research/twee/internal/rpc"
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

	cellData, rpcErr := handleCell(te, mustJSON(t, rpc.CellArgs{X: 0, Y: 0}))
	if rpcErr != nil {
		t.Fatalf("handleCell: %+v", rpcErr)
	}
	if got := cellData.(engine.Cell).Text; got != "h" {
		t.Fatalf("cell text = %q, want h", got)
	}

	regionData, rpcErr := handleRegion(te, mustJSON(t, rpc.RegionArgs{X: 0, Y: 0, W: 5, H: 1}))
	if rpcErr != nil {
		t.Fatalf("handleRegion: %+v", rpcErr)
	}
	region := regionData.([][]engine.Cell)
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
	region := data.([][]engine.Cell)
	if len(region) != 1 || len(region[0]) != 0 {
		t.Fatalf("region = %#v, want one empty row", region)
	}
}

func cellsText(cells []engine.Cell) string {
	var b strings.Builder
	for _, c := range cells {
		b.WriteString(c.Text)
	}
	return b.String()
}
