package tuitest

import (
	"strings"
	"testing"

	"github.com/paulsmith/twee/internal/input"
)

func TestMousePublicAliases(t *testing.T) {
	if LeftButton != input.ButtonLeft || MiddleButton != input.ButtonMiddle || RightButton != input.ButtonRight {
		t.Fatal("public mouse button aliases do not match internal values")
	}
	if ShiftModifier != input.ModifierShift || AltModifier != input.ModifierAlt || CtrlModifier != input.ModifierCtrl {
		t.Fatal("public mouse modifier aliases do not match internal values")
	}
	if ScrollUp != input.ScrollUp || ScrollDown != input.ScrollDown {
		t.Fatal("public scroll direction aliases do not match internal values")
	}
}

func TestApplyMouseOptions(t *testing.T) {
	cfg, err := applyMouseOptions("click", mouseGestureClick, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.button != LeftButton || len(cfg.modifiers) != 0 {
		t.Fatalf("defaults = %+v, want left with no modifiers", cfg)
	}

	cfg, err = applyMouseOptions("drag", mouseGestureDrag, []MouseOption{
		WithButton(RightButton),
		WithMouseModifiers(CtrlModifier, ShiftModifier),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.button != RightButton ||
		len(cfg.modifiers) != 2 ||
		cfg.modifiers[0] != CtrlModifier ||
		cfg.modifiers[1] != ShiftModifier {
		t.Fatalf("configured options = %+v", cfg)
	}
}

func TestMouseMethodsRejectInapplicableOptions(t *testing.T) {
	te := &Term{}
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"hover button", func() error { return te.Hover(1, 2, WithButton(RightButton)) }},
		{"scroll button", func() error {
			return te.Scroll(1, 2, ScrollDown, 1, WithButton(RightButton))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil || !strings.Contains(err.Error(), "WithButton is not applicable") {
				t.Fatalf("error = %v, want inapplicable WithButton", err)
			}
		})
	}
}

func TestMouseOptionsRejectInvalidAndDuplicateValues(t *testing.T) {
	tests := []struct {
		name string
		opts []MouseOption
		want string
	}{
		{
			name: "unknown button",
			opts: []MouseOption{WithButton(MouseButton(99))},
			want: "unknown mouse button",
		},
		{
			name: "nil option",
			opts: []MouseOption{nil},
			want: "nil mouse option",
		},
		{
			name: "duplicate button options",
			opts: []MouseOption{WithButton(LeftButton), WithButton(RightButton)},
			want: "WithButton supplied more than once",
		},
		{
			name: "unknown modifier",
			opts: []MouseOption{WithMouseModifiers(MouseModifier(99))},
			want: "unknown mouse modifier",
		},
		{
			name: "duplicate modifiers in one option",
			opts: []MouseOption{WithMouseModifiers(AltModifier, AltModifier)},
			want: "duplicate mouse modifier",
		},
		{
			name: "duplicate modifiers across options",
			opts: []MouseOption{
				WithMouseModifiers(CtrlModifier),
				WithMouseModifiers(CtrlModifier),
			},
			want: "duplicate mouse modifier",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyMouseOptions("click", mouseGestureClick, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
