package engine

import "testing"

func TestLinkHealthTrackerRequiresConsecutiveFailures(t *testing.T) {
	tracker := linkHealthTracker{}

	available, changed, failures := tracker.observe(13, true, false)
	if !available || changed || failures != 1 {
		t.Fatalf("first miss = (%t, %t, %d), want (true, false, 1)", available, changed, failures)
	}

	available, changed, failures = tracker.observe(13, true, true)
	if !available || changed || failures != 0 {
		t.Fatalf("success after miss = (%t, %t, %d), want (true, false, 0)", available, changed, failures)
	}

	available, changed, failures = tracker.observe(13, true, false)
	if !available || changed || failures != 1 {
		t.Fatalf("new first miss = (%t, %t, %d), want (true, false, 1)", available, changed, failures)
	}

	available, changed, failures = tracker.observe(13, true, false)
	if available || !changed || failures != 2 {
		t.Fatalf("second consecutive miss = (%t, %t, %d), want (false, true, 2)", available, changed, failures)
	}

	available, changed, failures = tracker.observe(13, false, true)
	if !available || !changed || failures != 0 {
		t.Fatalf("success after down state = (%t, %t, %d), want (true, true, 0)", available, changed, failures)
	}
}
