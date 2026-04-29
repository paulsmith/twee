package tuitest

// Expect* helpers wrap the corresponding WaitFor* with the default
// timeout and call t.Fatalf on failure. They require Run(t, ...).

// ExpectText fails the test if s does not appear within the default timeout.
func (te *Term) ExpectText(s string, opts ...WaitOption) {
	if te.t == nil {
		panic("tuitest: ExpectText requires Run(t, ...) construction")
	}
	te.t.Helper()
	if err := te.WaitForText(s, opts...); err != nil {
		te.t.Fatal(err)
	}
}

// ExpectNoText fails if s is currently on screen and remains so within
// the default timeout.
func (te *Term) ExpectNoText(s string, opts ...WaitOption) {
	if te.t == nil {
		panic("tuitest: ExpectNoText requires Run(t, ...) construction")
	}
	te.t.Helper()
	if err := te.WaitForNoText(s, opts...); err != nil {
		te.t.Fatal(err)
	}
}

// ExpectCursorAt fails if the cursor is not at (col, row).
func (te *Term) ExpectCursorAt(col, row int) {
	if te.t == nil {
		panic("tuitest: ExpectCursorAt requires Run(t, ...) construction")
	}
	te.t.Helper()
	c := te.Cursor()
	if c.Col != col || c.Row != row {
		te.t.Fatalf("cursor at (%d,%d), want (%d,%d)\n%s",
			c.Col, c.Row, col, row, te.Diagnostic())
	}
}
