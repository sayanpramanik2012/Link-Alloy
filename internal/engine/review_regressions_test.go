package engine

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestUDPDialDoesNotBlockCapturedPacketPump(t *testing.T) {
	entered, unblock := make(chan struct{}), make(chan struct{})
	f := newHardeningForwarder(t, NewScheduler([]Link{{Index: 1, Name: "test", Weight: 1, Available: true}}), func(ctx context.Context, _ uint32, _, _ string) (net.Conn, error) {
		close(entered)
		select {
		case <-unblock:
			return nil, errors.New("controlled dial failure")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	defer close(unblock)
	returned := make(chan error, 1)
	go func() {
		_, _, err := f.InjectCaptured(testUDPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.8"), 56789, 53, []byte("query")))
		returned <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("dial was not attempted")
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("one UDP dial stalled the shared captured-packet path")
	}
}

func TestRetryDoesNotResetNormalCohortRotation(t *testing.T) {
	s := NewScheduler([]Link{{Index: 1, Name: "Ethernet", Available: true, Weight: 1}, {Index: 2, Name: "Wi-Fi", Available: true, Weight: 1}})
	first, err := s.acquireCohort("content", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	retry, err := s.acquireCohort("content", map[uint32]struct{}{first.LinkIndex(): {}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Release()
	next, err := s.acquireCohort("content", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if next.LinkIndex() == first.LinkIndex() {
		t.Fatal("fallback dialing erased normal connection rotation")
	}
}

func TestUDPShutdownCancelsPendingDial(t *testing.T) {
	entered := make(chan struct{})
	f := newHardeningForwarder(t, NewScheduler([]Link{{Index: 1, Name: "test", Weight: 1, Available: true}}), func(ctx context.Context, _ uint32, _, _ string) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	flow, _, err := f.InjectCaptured(testUDPPacket4(netip.MustParseAddr("198.18.0.2"), netip.MustParseAddr("203.0.113.8"), 56790, 53, []byte("query")))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("dial was not attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	if f.Stats().ActiveUDP != 0 {
		t.Fatal("pending UDP worker leaked after shutdown")
	}
	if _, ok := f.flows.LookupVirtual(protocolUDP, flow.VirtualPort); ok {
		t.Fatal("pending UDP mapping leaked after shutdown")
	}
}
