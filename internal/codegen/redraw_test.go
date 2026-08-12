package codegen

import (
	"testing"
	"time"
)

func TestRedrawLifecycleCoalescesRequests(t *testing.T) {
	var redraws redrawLifecycle
	first := redraws.request()
	if first == nil {
		t.Fatal("first request did not arm redraw")
	}
	if second := redraws.request(); second != first {
		t.Fatal("second request replaced pending redraw")
	}

	select {
	case <-first:
		redraws.fired()
	case <-time.After(time.Second):
		t.Fatal("redraw did not fire")
	}
	if next := redraws.request(); next == nil || next == first {
		t.Fatal("request after frame did not arm a new redraw")
	}
	redraws.cancel()
}

func TestRedrawLifecycleCancelDisarmsRequest(t *testing.T) {
	var redraws redrawLifecycle
	pending := redraws.request()
	redraws.cancel()
	select {
	case <-pending:
		t.Fatal("canceled redraw fired")
	case <-time.After(2 * compositorFrameInterval):
	}
}
