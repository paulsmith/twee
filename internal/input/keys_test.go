package input

import (
	"bytes"
	"testing"
)

func TestEncodeNamedKeys(t *testing.T) {
	tests := []struct {
		name string
		key  Key
		want []byte
	}{
		{"none", KeyNone, nil},
		{"enter", KeyEnter, []byte{'\r'}},
		{"escape", KeyEscape, []byte{0x1b}},
		{"tab", KeyTab, []byte{'\t'}},
		{"backspace", KeyBackspace, []byte{0x7f}},
		{"delete", KeyDelete, []byte("\x1b[3~")},
		{"up", KeyUp, []byte("\x1b[A")},
		{"down", KeyDown, []byte("\x1b[B")},
		{"left", KeyLeft, []byte("\x1b[D")},
		{"right", KeyRight, []byte("\x1b[C")},
		{"home", KeyHome, []byte("\x1b[H")},
		{"end", KeyEnd, []byte("\x1b[F")},
		{"page up", KeyPageUp, []byte("\x1b[5~")},
		{"page down", KeyPageDown, []byte("\x1b[6~")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Encode(tt.key); !bytes.Equal(got, tt.want) {
				t.Fatalf("Encode(%v) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestCtrlNormalizesCase(t *testing.T) {
	for _, letter := range []byte{'C', 'c'} {
		if got := Encode(Ctrl(letter)); !bytes.Equal(got, []byte{0x03}) {
			t.Fatalf("Encode(Ctrl(%q)) = %q, want Ctrl-C", letter, got)
		}
		if got := Name(Ctrl(letter)); got != "Ctrl+C" {
			t.Fatalf("Name(Ctrl(%q)) = %q, want Ctrl+C", letter, got)
		}
	}
}

func TestEncodeCursorKeysHonorsApplicationCursorMode(t *testing.T) {
	tests := []struct {
		key  Key
		want string
	}{
		{KeyUp, "\x1bOA"},
		{KeyDown, "\x1bOB"},
		{KeyLeft, "\x1bOD"},
		{KeyRight, "\x1bOC"},
	}
	for _, tt := range tests {
		if got := EncodeWithModes(tt.key, KeyModes{ApplicationCursor: true}); !bytes.Equal(got, []byte(tt.want)) {
			t.Errorf("EncodeWithModes(%s, DECCKM) = %q, want %q", Name(tt.key), got, tt.want)
		}
	}
	if got := EncodeWithModes(KeyDelete, KeyModes{ApplicationCursor: true}); !bytes.Equal(got, []byte("\x1b[3~")) {
		t.Errorf("Delete under DECCKM = %q, want unchanged CSI sequence", got)
	}
}

func TestParseKnownKeysAndAliases(t *testing.T) {
	tests := []struct {
		name string
		want Key
	}{
		{"Enter", KeyEnter},
		{"Escape", KeyEscape},
		{"Esc", KeyEscape},
		{"Tab", KeyTab},
		{"Backspace", KeyBackspace},
		{"Delete", KeyDelete},
		{"Del", KeyDelete},
		{"Up", KeyUp},
		{"Down", KeyDown},
		{"Left", KeyLeft},
		{"Right", KeyRight},
		{"Home", KeyHome},
		{"End", KeyEnd},
		{"PageUp", KeyPageUp},
		{"PgUp", KeyPageUp},
		{"PageDown", KeyPageDown},
		{"PgDn", KeyPageDown},
		{"Ctrl+C", Ctrl('C')},
		{"Ctrl+c", Ctrl('C')},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.name)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.name, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	for _, name := range []string{"", "Space", "Ctrl+", "Ctrl+1", "Ctrl+Escape"} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(name)
			if err == nil {
				t.Fatalf("Parse(%q) unexpectedly succeeded with %v", name, got)
			}
			if got != KeyNone {
				t.Fatalf("Parse(%q) key = %v, want KeyNone", name, got)
			}
		})
	}
}

func TestNameUnknown(t *testing.T) {
	if got := Name(Key(99)); got != "Unknown" {
		t.Fatalf("Name(99) = %q, want Unknown", got)
	}
}

func TestEncodePasteWrapsBracketedPasteMarkers(t *testing.T) {
	got := EncodePaste("hello\nworld")
	want := []byte("\x1b[200~hello\nworld\x1b[201~")
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodePaste = %q, want %q", got, want)
	}
}
