package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FindMatch describes a visible-screen match in display-cell coordinates.
type FindMatch struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"`
	H    int    `json:"h"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// FindMatches searches one immutable snapshot. Byte offsets from Go's string
// matcher are translated back to display cells, including wide glyphs.
func FindMatches(s Snapshot, pattern string, regex bool) ([]FindMatch, error) {
	if pattern == "" && !regex {
		return nil, fmt.Errorf("text or regex required")
	}
	var re *regexp.Regexp
	if regex {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
	}
	matches := make([]FindMatch, 0)
	for y, line := range s.Lines {
		ln := newSearchLine(line)
		if regex {
			for _, idx := range re.FindAllStringIndex(ln.text, -1) {
				matches = append(matches, ln.match(y, idx[0], idx[1]))
			}
			continue
		}
		for start := 0; ; {
			i := strings.Index(ln.text[start:], pattern)
			if i < 0 {
				break
			}
			matches = append(matches, ln.match(y, start+i, start+i+len(pattern)))
			start += i + len(pattern)
		}
	}
	return matches, nil
}

type searchLine struct {
	text     string
	spans    []searchCellSpan
	finalCol int
}

type searchCellSpan struct{ byteStart, byteEnd, colStart, colEnd int }

func newSearchLine(line Line) searchLine {
	var b strings.Builder
	spans := make([]searchCellSpan, 0, len(line.Cells))
	for col, cell := range line.Cells {
		if cell.Width == 0 {
			continue
		}
		text := cell.Text
		if text == "" {
			text = " "
		}
		start := b.Len()
		b.WriteString(text)
		spans = append(spans, searchCellSpan{start, b.Len(), col, col + cell.Width})
	}
	text := strings.TrimRight(b.String(), " ")
	ln := searchLine{text: text, spans: spans}
	ln.finalCol = ln.endCol(len(text))
	return ln
}

func (ln searchLine) match(row, byteStart, byteEnd int) FindMatch {
	startCol, endCol := ln.startCol(byteStart), ln.endCol(byteEnd)
	if byteStart == byteEnd {
		endCol = startCol
	}
	return FindMatch{X: startCol, Y: row, W: endCol - startCol, H: 1, Line: row, Text: ln.text[byteStart:byteEnd]}
}

func (ln searchLine) startCol(offset int) int {
	if offset <= 0 {
		if len(ln.spans) > 0 {
			return ln.spans[0].colStart
		}
		return 0
	}
	if offset >= len(ln.text) {
		return ln.finalCol
	}
	for _, span := range ln.spans {
		switch {
		case offset <= span.byteStart:
			return span.colStart
		case offset < span.byteEnd:
			return span.colStart
		case offset == span.byteEnd:
			return span.colEnd
		}
	}
	return ln.finalCol
}

func (ln searchLine) endCol(offset int) int {
	if offset <= 0 {
		return 0
	}
	for _, span := range ln.spans {
		switch {
		case offset <= span.byteStart:
			return span.colStart
		case offset <= span.byteEnd:
			return span.colEnd
		}
	}
	if len(ln.spans) == 0 {
		return 0
	}
	return ln.spans[len(ln.spans)-1].colEnd
}

func selectFindMatch(matches []FindMatch, selection string) (FindMatch, string, error) {
	var match FindMatch
	selected := selection
	switch selection {
	case "":
		switch len(matches) {
		case 0:
			return FindMatch{}, "exactly_one", findDecisionError("NOT_FOUND", "pattern was not found", matches, "exactly_one")
		case 1:
			match, selected = matches[0], "exactly_one"
		default:
			return FindMatch{}, "exactly_one", findDecisionError("AMBIGUOUS_MATCH", "pattern matched more than once", matches, "exactly_one")
		}
	case "first":
		if len(matches) == 0 {
			return FindMatch{}, selection, findDecisionError("NOT_FOUND", "pattern was not found", matches, selection)
		}
		match = matches[0]
	case "last":
		if len(matches) == 0 {
			return FindMatch{}, selection, findDecisionError("NOT_FOUND", "pattern was not found", matches, selection)
		}
		match = matches[len(matches)-1]
	default:
		n, err := strconv.Atoi(selection)
		if err != nil || n <= 0 {
			return FindMatch{}, selection, findDecisionError("INVALID_SELECTION", "selection must be first, last, or a positive match number", matches, selection)
		}
		if n > len(matches) {
			return FindMatch{}, selection, findDecisionError("INVALID_SELECTION", "selected match is out of range", matches, selection)
		}
		match, selected = matches[n-1], strconv.Itoa(n)
	}
	if match.W <= 0 || match.H <= 0 {
		err := findDecisionError("INVALID_SELECTION", "selected match has no display cells to click", matches, selected)
		err.(*RequestError).Details.(map[string]any)["match"] = match
		return FindMatch{}, selected, err
	}
	return match, selected, nil
}

func findDecisionError(code, message string, matches []FindMatch, selection string) error {
	const sampleLimit = 10
	sample := matches
	if len(sample) > sampleLimit {
		sample = sample[:sampleLimit]
	}
	return &RequestError{Kind: RequestErrorInvalidArgument, Code: code, Message: message, Details: map[string]any{
		"match_count": len(matches), "selection": selection, "matches": sample,
	}}
}
