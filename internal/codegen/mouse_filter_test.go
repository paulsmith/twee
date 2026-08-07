package codegen

import "testing"

func TestStatusMouseFilterRemovesOnlyStatusReports(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"sgr", []byte("a\x1b[<0;1;24Mb\x1b[<0;1;23M"), []byte("ab\x1b[<0;1;23M")},
		{"urxvt", []byte("\x1b[0;1;24M\x1b[0;1;23M"), []byte("\x1b[0;1;23M")},
		{"x10", []byte{'x', '\x1b', '[', 'M', 32, 33, 56, 'y', '\x1b', '[', 'M', 32, 33, 55}, []byte{'x', 'y', '\x1b', '[', 'M', 32, 33, 55}},
		{"malformed", []byte("\x1b[<0;1;xM"), []byte("\x1b[<0;1;xM")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f statusMouseFilter
			if got := f.Feed(tt.in, 23, true); string(got) != string(tt.want) {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestStatusMouseFilterHandlesChunksAndHiddenStatus(t *testing.T) {
	var f statusMouseFilter
	if got := f.Feed([]byte("\x1b["), 23, true); len(got) != 0 {
		t.Fatalf("CSI prefix=%q", got)
	}
	if got := f.Feed([]byte("<0;1;24M"), 23, true); len(got) != 0 {
		t.Fatalf("split status mouse=%q", got)
	}

	var f2 statusMouseFilter
	if got := f2.Feed([]byte("\x1b[<0;1;2"), 23, true); len(got) != 0 {
		t.Fatalf("partial=%q", got)
	}
	if got := f2.Feed([]byte("4M"), 23, true); len(got) != 0 {
		t.Fatalf("status mouse=%q", got)
	}
	if got := f2.Feed([]byte("\x1b[<0;1;24M"), 23, false); string(got) != "\x1b[<0;1;24M" {
		t.Fatalf("hidden status filtered mouse=%q", got)
	}
}
