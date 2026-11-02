package engine

type linkHealthTracker struct {
	failures map[uint32]int
}

func (t *linkHealthTracker) observe(index uint32, currentlyAvailable, probeSucceeded bool) (available, changed bool, consecutiveFailures int) {
	if probeSucceeded {
		if t.failures != nil {
			delete(t.failures, index)
		}
		return true, !currentlyAvailable, 0
	}

	if t.failures == nil {
		t.failures = make(map[uint32]int)
	}
	t.failures[index]++
	consecutiveFailures = t.failures[index]
	if consecutiveFailures < linkHealthFailures {
		return currentlyAvailable, false, consecutiveFailures
	}
	return false, currentlyAvailable, consecutiveFailures
}
