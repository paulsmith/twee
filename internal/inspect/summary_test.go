package inspect

import (
	"testing"
	"time"

	"github.com/paulsmith/twee/internal/trace"
	"github.com/paulsmith/twee/internal/tracebundle"
)

func TestSummarizeUsesManifestDurationAndCountsEvents(t *testing.T) {
	start := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	stop := start.Add(1234 * time.Millisecond)
	s := Summarize("foo.twee", tracebundle.Bundle{
		Manifest: trace.Manifest{
			Version:   1,
			Command:   []string{"vim", "file.txt"},
			Cols:      80,
			Rows:      24,
			StartedAt: start,
			StoppedAt: stop,
		},
		Events: []tracebundle.Event{
			{TMS: 0, Type: "output"},
			{TMS: 10, Type: "input", Kind: "key"},
			{TMS: 20, Type: "input", Kind: "type"},
			{TMS: 30, Type: "resize", Cols: 100, Rows: 40},
			{TMS: 1210, Type: "mystery"},
			{TMS: 1210, Type: "exit", Code: 7},
		},
		MaxCols: 100,
		MaxRows: 40,
	})

	if s.Path != "foo.twee" || s.Version != 1 {
		t.Fatalf("identity = %q/%d, want foo.twee/1", s.Path, s.Version)
	}
	if got := s.Command; len(got) != 2 || got[0] != "vim" || got[1] != "file.txt" {
		t.Fatalf("command = %#v", got)
	}
	if s.Duration != "1.234s" || s.DurationMS != 1234 {
		t.Fatalf("duration = %q/%d, want 1.234s/1234", s.Duration, s.DurationMS)
	}
	if s.EventSpanMS != 1210 {
		t.Fatalf("event span = %d, want 1210", s.EventSpanMS)
	}
	if s.StartedAt == nil || !s.StartedAt.Equal(start) {
		t.Fatalf("started_at = %v, want %v", s.StartedAt, start)
	}
	if s.StoppedAt == nil || !s.StoppedAt.Equal(stop) {
		t.Fatalf("stopped_at = %v, want %v", s.StoppedAt, stop)
	}
	if s.Terminal.Cols != 80 || s.Terminal.Rows != 24 || s.Terminal.MaxCols != 100 || s.Terminal.MaxRows != 40 {
		t.Fatalf("terminal = %+v", s.Terminal)
	}
	if s.Events.Total != 6 {
		t.Fatalf("events total = %d, want 6", s.Events.Total)
	}
	for typ, want := range map[string]int{"output": 1, "input": 2, "resize": 1, "mystery": 1, "exit": 1} {
		if got := s.Events.ByType[typ]; got != want {
			t.Fatalf("by_type[%q] = %d, want %d", typ, got, want)
		}
	}
	for kind, want := range map[string]int{"key": 1, "type": 1} {
		if got := s.Events.InputByKind[kind]; got != want {
			t.Fatalf("input_by_kind[%q] = %d, want %d", kind, got, want)
		}
	}
	if !s.Exit.Recorded || s.Exit.Code == nil || *s.Exit.Code != 7 {
		t.Fatalf("exit = %+v, want recorded code 7", s.Exit)
	}
}

func TestSummarizeNoExitEvent(t *testing.T) {
	s := Summarize("no-exit.twee", tracebundle.Bundle{
		Manifest: trace.Manifest{Version: 1, Cols: 80, Rows: 24},
		Events:   []tracebundle.Event{{TMS: 50, Type: "output"}},
		MaxCols:  80,
		MaxRows:  24,
	})

	if s.Exit.Recorded {
		t.Fatalf("exit recorded = true, want false")
	}
	if s.Exit.Code != nil {
		t.Fatalf("exit code = %v, want nil", *s.Exit.Code)
	}
}

func TestSummarizeFallsBackToEventSpanForMissingOrInvalidManifestTimes(t *testing.T) {
	for _, tt := range []struct {
		name      string
		startedAt time.Time
		stoppedAt time.Time
	}{
		{name: "missing times"},
		{
			name:      "negative manifest duration",
			startedAt: time.Date(2026, 6, 23, 12, 0, 2, 0, time.UTC),
			stoppedAt: time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Summarize("fallback.twee", tracebundle.Bundle{
				Manifest: trace.Manifest{
					Version:   1,
					Cols:      80,
					Rows:      24,
					StartedAt: tt.startedAt,
					StoppedAt: tt.stoppedAt,
				},
				Events:  []tracebundle.Event{{TMS: 250, Type: "output"}, {TMS: 1250, Type: "exit"}},
				MaxCols: 80,
				MaxRows: 24,
			})

			if s.Duration != "1.25s" || s.DurationMS != 1250 || s.EventSpanMS != 1250 {
				t.Fatalf("duration = %q/%d, span=%d, want 1.25s/1250 span 1250", s.Duration, s.DurationMS, s.EventSpanMS)
			}
		})
	}
}
