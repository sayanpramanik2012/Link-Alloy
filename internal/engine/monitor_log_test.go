package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogQueueIsBoundedAndRotationRetainsBudget(t *testing.T) {
	l := &eventLogger{queue: make(chan EngineEvent, 2)}
	for range 100 {
		l.enqueue(EngineEvent{Message: "test"})
	}
	if l.dropped.Load() != 98 {
		t.Fatal("log queue is not bounded")
	}
	p := filepath.Join(t.TempDir(), "engine.jsonl")
	if e := os.WriteFile(p, make([]byte, logFileBudget), 0600); e != nil {
		t.Fatal(e)
	}
	l.path = func() (string, error) { return p, nil }
	l.secure = func(string) error { return nil }
	if e := l.write(EngineEvent{Message: "rotated"}); e != nil {
		t.Fatal(e)
	}
	if st, e := os.Stat(p + ".1"); e != nil || st.Size() != logFileBudget {
		t.Fatal("log rotation failed")
	}
	if st, e := os.Stat(p); e != nil || st.Size() > 1024 {
		t.Fatal("new log unbounded")
	}
}
func TestMonitorReconcilesHotplugAndKeepsFamiliesSeparate(t *testing.T) {
	var phase atomic.Int32
	c := &TunnelController{preferences: map[string]linkPreference{}, healthPeriod: 5 * time.Millisecond}
	c.listLinks = func() ([]Link, error) {
		idx := uint32(1)
		if phase.Load() > 0 {
			idx = 2
		}
		return []Link{{Index: idx, Name: "physical", Weight: 1, FamiliesKnown: true}}, nil
	}
	c.probe = func(_ context.Context, _ uint32, v6 bool) error {
		if v6 {
			return errors.New("no IPv6")
		}
		return nil
	}
	s := NewScheduler(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.monitorLinks(ctx, s) }()
	defer func() { cancel(); <-done }()
	wait := func(idx uint32) {
		t.Helper()
		deadline := time.After(time.Second)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-deadline:
				t.Fatal("health monitor did not reconcile")
			case <-ticker.C:
				links := s.Snapshots()
				if len(links) == 1 && links[0].Index == idx && links[0].IPv4Available {
					if links[0].IPv6Available {
						t.Fatal("IPv4 probe incorrectly enabled IPv6")
					}
					return
				}
			}
		}
	}
	wait(1)
	phase.Store(1)
	wait(2)
}
