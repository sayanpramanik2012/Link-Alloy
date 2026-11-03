package engine

import (
	"errors"
	"testing"
	"time"
)

func TestAcquireUsesConfiguredCapacityWeights(t *testing.T) {
	scheduler := NewScheduler([]Link{
		{Index: 4, Name: "Ethernet", Weight: 4, Available: true},
		{Index: 9, Name: "Wi-Fi", Weight: 1, Available: true},
	})
	counts := map[uint32]int{}
	leases := make([]*Lease, 0, 5)
	for range 5 {
		lease, err := scheduler.Acquire()
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		counts[lease.LinkIndex()]++
		leases = append(leases, lease)
	}
	for _, lease := range leases {
		lease.Release()
	}
	if counts[4] != 4 || counts[9] != 1 {
		t.Fatalf("weighted assignments = %#v, want 4 Ethernet and 1 Wi-Fi", counts)
	}
}

func TestAcquireUsesWeightedRoundRobinAcrossNewFlows(t *testing.T) {
	scheduler := NewScheduler([]Link{{Index: 4, Name: "Ethernet", Weight: 3, Available: true}, {Index: 9, Name: "Wi-Fi", Weight: 1, Available: true}})
	counts := map[uint32]int{}
	for range 40 {
		lease, err := scheduler.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		counts[lease.LinkIndex()]++
		lease.Release()
	}
	if counts[4] != 30 || counts[9] != 10 {
		t.Fatalf("weighted flow allocation = %#v, want 30/10", counts)
	}
}

func TestAcquirePrefersUnderutilizedLinkForNewFlow(t *testing.T) {
	scheduler := NewScheduler([]Link{
		{Index: 4, Name: "Ethernet", Weight: 1, Available: true},
		{Index: 9, Name: "Wi-Fi", Weight: 1, Available: true},
	})
	first, err := scheduler.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first.reportAt(now, 0, 20<<20)
	next, err := scheduler.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if next.LinkIndex() == first.LinkIndex() {
		t.Fatalf("new flow stayed on saturated link %d", next.LinkIndex())
	}
	first.Release()
	next.Release()
}

func TestAcquireSkipsUnavailableAndReturnsWhenAllLinksAreDown(t *testing.T) {
	scheduler := NewScheduler([]Link{
		{Index: 4, Name: "Ethernet", Weight: 4, Available: false},
		{Index: 9, Name: "Wi-Fi", Weight: 1, Available: true},
	})
	lease, err := scheduler.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if lease.LinkIndex() != 9 {
		t.Fatalf("selected interface = %d, want Wi-Fi interface 9", lease.LinkIndex())
	}
	lease.Release()
	scheduler.SetAvailable(9, false)
	if _, err := scheduler.Acquire(); !errors.Is(err, ErrNoHealthyLink) {
		t.Fatalf("Acquire() error = %v, want ErrNoHealthyLink", err)
	}
}

func TestLeaseReleaseIsIdempotentAndReportsTraffic(t *testing.T) {
	scheduler := NewScheduler([]Link{{Index: 4, Name: "Ethernet", Weight: 1, Available: true}})
	lease, err := scheduler.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	lease.Report(1024, 2048)
	lease.Release()
	lease.Release()
	snapshot := scheduler.Snapshots()[0]
	if snapshot.ActiveConnections != 0 {
		t.Fatalf("active connections = %d, want 0", snapshot.ActiveConnections)
	}
	if snapshot.BytesSent != 1024 || snapshot.BytesReceived != 2048 {
		t.Fatalf("traffic totals = %d/%d, want 1024/2048", snapshot.BytesSent, snapshot.BytesReceived)
	}
}

func TestPollingAndDemandDoNotChangeCapacityWeights(t *testing.T) {
	scheduler := NewScheduler([]Link{
		{Index: 4, Name: "Ethernet", Weight: 1, Available: true},
		{Index: 9, Name: "Wi-Fi", Weight: 1, Available: true},
	})
	now := time.Now()
	lease, err := scheduler.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	lease.reportAt(now, 0, 8_000_000)
	for range 100 {
		for _, snapshot := range scheduler.snapshotsAt(now.Add(time.Millisecond)) {
			if snapshot.Weight != 1 {
				t.Fatal("status polling changed allocation weight")
			}
			if snapshot.RecvBytesPerSec > 4_000_000 {
				t.Fatal("rate exceeded the fixed two-second average")
			}
		}
	}
	if scheduler.snapshotsAt(now.Add(5 * time.Second))[0].RecvBytesPerSec != 0 {
		t.Fatal("expired samples still contribute")
	}
}

func TestFamilyEligibilityAndLinkControls(t *testing.T) {
	s := NewScheduler([]Link{{Index: 1, Name: "v4", Weight: 1, Available: true, FamiliesKnown: true, IPv4Available: true}, {Index: 2, Name: "v6", Weight: 1, Available: true, FamiliesKnown: true, IPv6Available: true}})
	l, e := s.AcquireExcludingForFamily(nil, true)
	if e != nil || l.LinkIndex() != 2 {
		t.Fatal("IPv6 selected an IPv4-only adapter")
	}
	l.Release()
	if e = s.Configure(2, false, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AcquireExcludingForFamily(nil, true); !errors.Is(e, ErrNoHealthyLink) {
		t.Fatal("disabled link accepted a new flow")
	}
	s.Reconcile([]Link{{Index: 3, Name: "hotplug", Weight: 1, Available: true}})
	l, e = s.Acquire()
	if e != nil || l.LinkIndex() != 3 {
		t.Fatal("hotplug reconciliation failed")
	}
	l.Release()
}
