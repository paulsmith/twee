package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMouseArgsPreserveRequiredZeroAndOptionalTicksPresence(t *testing.T) {
	zero := 0
	click, err := json.Marshal(ClickArgs{X: &zero, Y: &zero})
	if err != nil {
		t.Fatalf("marshal click: %v", err)
	}
	if got := string(click); !strings.Contains(got, `"x":0`) || !strings.Contains(got, `"y":0`) {
		t.Fatalf("click JSON = %s, want explicit zero coordinates", got)
	}

	scroll, err := json.Marshal(ScrollArgs{X: &zero, Y: &zero, Direction: "down"})
	if err != nil {
		t.Fatalf("marshal default scroll: %v", err)
	}
	if strings.Contains(string(scroll), `"ticks"`) {
		t.Fatalf("default scroll JSON = %s, want ticks omitted", scroll)
	}

	scroll, err = json.Marshal(ScrollArgs{
		X: &zero, Y: &zero, Direction: "down", Ticks: &zero,
	})
	if err != nil {
		t.Fatalf("marshal explicit-zero scroll: %v", err)
	}
	if !strings.Contains(string(scroll), `"ticks":0`) {
		t.Fatalf("explicit-zero scroll JSON = %s, want ticks present", scroll)
	}
}

func TestModeDataAlwaysIncludesRawMouseBooleans(t *testing.T) {
	raw, err := json.Marshal(ModeData{})
	if err != nil {
		t.Fatalf("marshal mode data: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode mode data: %v", err)
	}
	for _, key := range []string{
		"mouse_tracking_x10",
		"mouse_tracking_normal",
		"mouse_tracking_button",
		"mouse_tracking_any",
		"mouse_format_utf8",
		"mouse_format_sgr",
		"mouse_format_urxvt",
		"mouse_format_sgr_pixels",
	} {
		value, ok := fields[key]
		if !ok {
			t.Errorf("ModeData JSON missing %q: %s", key, raw)
			continue
		}
		if value != false {
			t.Errorf("ModeData JSON %q = %#v, want false", key, value)
		}
	}
}
