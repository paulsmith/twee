package engine

import "testing"

func TestFindMatchesDisplayCells(t *testing.T) {
	s := Snapshot{Lines: []Line{{Cells: []Cell{
		{Text: "A", Width: 1}, {Text: "界", Width: 2}, {}, {Text: "é", Width: 1},
	}}}}
	tests := []struct {
		pattern string
		regex   bool
		x, w    int
		text    string
	}{
		{"界", false, 1, 2, "界"},
		{"é", false, 3, 1, "é"},
		{"界e.", true, 1, 3, "界é"},
	}
	for _, tt := range tests {
		matches, err := FindMatches(s, tt.pattern, tt.regex)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 || matches[0].X != tt.x || matches[0].W != tt.w || matches[0].Text != tt.text {
			t.Fatalf("FindMatches(%q) = %#v", tt.pattern, matches)
		}
	}
}

func TestSelectFindMatchPolicies(t *testing.T) {
	matches := []FindMatch{{X: 1, W: 1, H: 1}, {X: 5, W: 1, H: 1}, {X: 9, W: 1, H: 1}}
	for selection, wantX := range map[string]int{"first": 1, "last": 9, "2": 5} {
		got, _, err := selectFindMatch(matches, selection)
		if err != nil || got.X != wantX {
			t.Fatalf("select %q = %+v, %v", selection, got, err)
		}
	}
	for _, selection := range []string{"", "4", "zero"} {
		if _, _, err := selectFindMatch(matches, selection); err == nil {
			t.Fatalf("select %q unexpectedly succeeded", selection)
		}
	}
	if _, _, err := selectFindMatch(nil, "first"); err == nil {
		t.Fatal("zero matches unexpectedly succeeded")
	}
	if _, _, err := selectFindMatch(nil, "1"); err == nil || err.(*RequestError).Code != "INVALID_SELECTION" {
		t.Fatalf("numeric zero-match selection = %v", err)
	}
}

func TestZeroWidthRegexMatchesRemainExplorableButCannotBeClicked(t *testing.T) {
	s := Snapshot{Lines: []Line{{Cells: textCellLine("abc")}}}
	for _, tt := range []struct {
		pattern, selection string
		wantX              int
	}{
		{"", "first", 0},
		{"^", "", 0},
		{"$", "", 3},
	} {
		matches, err := FindMatches(s, tt.pattern, true)
		if err != nil || len(matches) == 0 {
			t.Fatalf("FindMatches(%q) = %#v, %v", tt.pattern, matches, err)
		}
		match := matches[0]
		if tt.pattern == "$" {
			match = matches[len(matches)-1]
		}
		if match.W != 0 || match.X != tt.wantX {
			t.Fatalf("exploratory %q match = %+v", tt.pattern, match)
		}
		_, _, err = selectFindMatch(matches, tt.selection)
		requestErr, ok := err.(*RequestError)
		if !ok || requestErr.Code != "INVALID_SELECTION" {
			t.Fatalf("select zero-width %q = %v", tt.pattern, err)
		}
		details := requestErr.Details.(map[string]any)
		if details["match"] == nil || details["selection"] == nil {
			t.Fatalf("zero-width details = %#v", details)
		}
	}
}

func textCellLine(text string) []Cell {
	cells := make([]Cell, 0, len(text))
	for _, r := range text {
		cells = append(cells, Cell{Text: string(r), Width: 1})
	}
	return cells
}
