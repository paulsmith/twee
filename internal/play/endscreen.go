package play

import (
	"strings"

	"github.com/paulsmith/twee/internal/engine"
)

// The hint names keys with the same verbs as the status-line key legend.
const (
	endScreenTitle = "End of playback"
	endScreenHint  = "r restart · q quit"
)

// overlayEndScreen dims snap's cells and draws a centered banner telling
// the user playback has ended. It mutates snap in place, so the caller
// must own the snapshot (engineSnapshot returns a deep copy).
func overlayEndScreen(snap *engine.Snapshot) {
	if snap.Cols <= 0 || snap.Rows <= 0 {
		return
	}
	for y := range snap.Lines {
		cells := snap.Lines[y].Cells
		for x := range cells {
			cells[x].Dim = true
		}
	}

	title := []rune(endScreenTitle)
	hint := []rune(endScreenHint)
	innerW := max(len(title), len(hint)) + 2
	const boxH = 4
	if innerW+2 <= snap.Cols && boxH <= snap.Rows {
		left := (snap.Cols - innerW - 2) / 2
		top := (snap.Rows - boxH) / 2
		hbar := []rune(strings.Repeat("─", innerW))
		blank := []rune(strings.Repeat(" ", innerW))
		writeCells(snap, left, top, boxRow('┌', hbar, '┐'), false)
		writeCells(snap, left, top+1, boxRow('│', blank, '│'), false)
		writeCells(snap, left, top+2, boxRow('│', blank, '│'), false)
		writeCells(snap, left, top+3, boxRow('└', hbar, '┘'), false)
		writeCells(snap, left+1+(innerW-len(title))/2, top+1, title, true)
		writeCells(snap, left+1+(innerW-len(hint))/2, top+2, hint, false)
		return
	}

	// Too narrow or short for the box: bare centered text, truncated.
	textRow := func(y int, text []rune) {
		if len(text) > snap.Cols {
			text = text[:snap.Cols]
		}
		writeCells(snap, (snap.Cols-len(text))/2, y, text, false)
	}
	if snap.Rows >= 2 {
		top := (snap.Rows - 2) / 2
		textRow(top, title)
		textRow(top+1, hint)
		return
	}
	textRow(0, title)
}

func boxRow(left rune, interior []rune, right rune) []rune {
	row := make([]rune, 0, len(interior)+2)
	row = append(row, left)
	row = append(row, interior...)
	return append(row, right)
}

// writeCells overwrites cells starting at (x, y) with default colors,
// extending short or missing lines so the banner is visible even when the
// model reported fewer cells than the frame size. Wide glyphs straddling
// the written range are blanked so no clipped halves or orphaned Width=0
// continuation cells remain next to it.
func writeCells(snap *engine.Snapshot, x, y int, text []rune, bold bool) {
	if y < 0 || y >= snap.Rows || len(text) == 0 {
		return
	}
	for y >= len(snap.Lines) {
		snap.Lines = append(snap.Lines, engine.Line{})
	}
	line := &snap.Lines[y]
	for i, r := range text {
		cx := x + i
		if cx < 0 || cx >= snap.Cols {
			continue
		}
		for cx >= len(line.Cells) {
			line.Cells = append(line.Cells, engine.Cell{Text: " ", Width: 1})
		}
		line.Cells[cx] = engine.Cell{Text: string(r), Width: 1, Bold: bold}
	}
	if i := x - 1; i >= 0 && i < len(line.Cells) && line.Cells[i].Width > 1 {
		line.Cells[i].Text, line.Cells[i].Width = " ", 1
	}
	if i := x + len(text); i >= 0 && i < len(line.Cells) && line.Cells[i].Width == 0 {
		line.Cells[i].Text, line.Cells[i].Width = " ", 1
	}
}
