package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestApplicationBurstUnaffectedByOtherTraffic(t *testing.T) {
	for _, n := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			links := make([]Link, n)
			for i := range links {
				links[i] = Link{Index: uint32(i + 1), Name: fmt.Sprint(i + 1), Weight: 1, Available: true}
			}
			s := NewScheduler(links)
			// Large unrelated upload and long-lived sockets must not dictate a new
			// application's content-connection placement.
			bg, _ := s.acquireCohort("background:443", nil, nil)
			bg.Report(100<<20, 0)
			defer bg.Release()
			counts := map[uint32]int{}
			var mu sync.Mutex
			var wg sync.WaitGroup
			for range n * 10 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					l, err := s.acquireCohort("download:443", nil, nil)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					counts[l.LinkIndex()]++
					mu.Unlock()
					l.Report(0, 1<<20)
					l.Release()
				}()
			}
			wg.Wait()
			for _, l := range links {
				if counts[l.Index] != 10 {
					t.Fatalf("cohort assignments: %v", counts)
				}
			}
		})
	}
}

func TestCohortWeightsReconcileAndFailure(t *testing.T) {
	s := NewScheduler([]Link{{Index: 1, Name: "a", Weight: 3, Available: true}, {Index: 2, Name: "b", Weight: 1, Available: true}})
	counts := map[uint32]int{}
	for i := 0; i < 40; i++ {
		// A normal monitor refresh must preserve allocation credits.
		s.Reconcile([]Link{{Index: 1, Name: "a", Weight: 1, Available: true}, {Index: 2, Name: "b", Weight: 1, Available: true}})
		l, err := s.acquireCohort("app", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		counts[l.LinkIndex()]++
		l.Release()
	}
	if counts[1] != 30 || counts[2] != 10 {
		t.Fatal(counts)
	}
	failed, err := s.acquireCohort("app", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.acquireCohort("app", map[uint32]struct{}{failed.LinkIndex(): {}}, nil)
	if err != nil || retry.LinkIndex() == failed.LinkIndex() {
		t.Fatal("fallback did not change links")
	}
	failed.Release()
	retry.Release()
	retry.Release()
	if err := s.Configure(1, false, 1); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		l, e := s.acquireCohort("app", nil, nil)
		if e != nil || l.LinkIndex() != 2 {
			t.Fatal("disabled link selected")
		}
		l.Release()
	}
	for _, l := range s.Snapshots() {
		if l.ActiveConnections != 0 {
			t.Fatal("leaked reservation")
		}
	}
}

func TestCohortCacheAndCreditsBounded(t *testing.T) {
	s := NewScheduler([]Link{{Index: 1, Name: "a", Weight: 1, Available: true}, {Index: 2, Name: "b", Weight: 1, Available: true}})
	for i := 0; i < 2200; i++ {
		l, e := s.acquireCohort(fmt.Sprint(i), nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		l.Release()
	}
	if len(s.cohorts) > 1024 {
		t.Fatal("unbounded cohort cache")
	}
	for range 1000 {
		l, _ := s.acquireCohort("app", nil, nil)
		l.Release()
	}
	for _, v := range s.cohorts["app"].credits {
		if v < -2 || v > 2 {
			t.Fatal("unbounded credit", v)
		}
	}
	before := s.cohorts["app"].credits[1]
	for range 100 {
		s.snapshotsAt(time.Now())
	}
	if s.cohorts["app"].credits[1] != before {
		t.Fatal("polling changed scheduling")
	}
}
