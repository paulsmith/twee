package tuitest

import "testing"

// Expect* helpers wrap the corresponding WaitFor* with the default
// timeout and call t.Fatalf on failure. They require Run(t, ...).

func (te *Term) requireTestingTB(method string) testing.TB {
	if te.t == nil {
		panic("tuitest: " + method + " requires Run(t, ...) construction")
	}
	te.t.Helper()
	return te.t
}

// ExpectText fails the test if s does not appear within the default timeout.
func (te *Term) ExpectText(s string, opts ...WaitOption) {
	t := te.requireTestingTB("ExpectText")
	if err := te.WaitForText(s, opts...); err != nil {
		t.Fatal(err)
	}
}

// ExpectNoText fails if s is currently on screen and remains so within
// the default timeout.
func (te *Term) ExpectNoText(s string, opts ...WaitOption) {
	t := te.requireTestingTB("ExpectNoText")
	if err := te.WaitForNoText(s, opts...); err != nil {
		t.Fatal(err)
	}
}

// ExpectCursorAt fails if the cursor is not at (col, row).
func (te *Term) ExpectCursorAt(col, row int) {
	t := te.requireTestingTB("ExpectCursorAt")
	c := te.Cursor()
	if c.Col != col || c.Row != row {
		t.Fatalf("cursor at (%d,%d), want (%d,%d)\n%s",
			c.Col, c.Row, col, row, te.Diagnostic())
	}
}
