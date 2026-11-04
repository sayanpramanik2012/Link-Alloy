package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestOwnerTableParsing(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		stride := 24
		if ipv6 {
			stride = 56
		}
		data := make([]byte, 4+stride)
		binary.LittleEndian.PutUint32(data[:4], 1)
		row := data[4:]
		if ipv6 {
			row[15] = 1
			row[39] = 2
			binary.BigEndian.PutUint16(row[20:22], 55123)
			binary.BigEndian.PutUint16(row[44:46], 443)
			binary.LittleEndian.PutUint32(row[48:52], 3)
			binary.LittleEndian.PutUint32(row[52:56], 1234)
		} else {
			copy(row[4:8], []byte{198, 18, 0, 2})
			copy(row[12:16], []byte{203, 0, 113, 5})
			binary.BigEndian.PutUint16(row[8:10], 55123)
			binary.BigEndian.PutUint16(row[16:18], 443)
			binary.LittleEndian.PutUint32(row[:4], 3)
			binary.LittleEndian.PutUint32(row[20:24], 1234)
		}
		rows := parseTCPOwners(data, ipv6)
		if len(rows) != 1 {
			t.Fatal("row missing")
		}
		for k, pid := range rows {
			if pid != 1234 || k.SourcePort != 55123 || k.DestinationPort != 443 || k.Destination.Is6() != ipv6 {
				t.Fatal(k, pid)
			}
		}
		binary.LittleEndian.PutUint32(data[:4], 100)
		if len(parseTCPOwners(data, ipv6)) != 0 {
			t.Fatal("malformed count accepted")
		}
	}
}

func TestOwnerResolverMatchesRealWindowsSocket(t *testing.T) {
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp4", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a := c.LocalAddr().(*net.TCPAddr)
	b := c.RemoteAddr().(*net.TCPAddr)
	source, _ := netip.AddrFromSlice(a.IP)
	dest, _ := netip.AddrFromSlice(b.IP)
	flow := Flow{Key: FlowKey{Protocol: protocolTCP, Source: source.Unmap(), SourcePort: uint16(a.Port), Destination: dest.Unmap(), DestinationPort: uint16(b.Port)}}
	r := ownerResolver{}
	owner := r.resolve(flow)
	if owner.PID != uint32(os.Getpid()) || owner.Instance == "" || owner.Name == "" {
		t.Fatalf("owner=%+v", owner)
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 && verifyUpstreamInterface(c, uint32(iface.Index)) == nil {
			verified = true
			break
		}
	}
	if !verified {
		t.Fatal("could not verify the real loopback binding")
	}
	if verifyUpstreamInterface(c, 0) == nil {
		t.Fatal("invalid selected interface accepted")
	}
}

func TestDiagnosticsBoundsAndCorrelation(t *testing.T) {
	r := flowRegistry{}
	for range 400 {
		id := r.begin(Flow{}, flowOwner{}, "tcp")
		r.end(id, "completed")
	}
	if len(r.flows) != 256 {
		t.Fatal("completed retention", len(r.flows))
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	id := r.begin(Flow{Key: FlowKey{SourcePort: 50000, DestinationPort: 443}}, flowOwner{PID: 42, Name: "test.exe"}, "tcp")
	r.connected(id, 13, a)
	r.report(id, 1000, 2<<20)
	snapshot := r.snapshots()[0]
	if snapshot.ID != id || snapshot.LinkIndex != 13 || snapshot.ProcessID != 42 || snapshot.BytesReceived != 2<<20 || snapshot.RecvBytesPerSec != 1<<20 {
		t.Fatal(snapshot)
	}
	r.end(id, "completed")
	for _, f := range r.snapshots() {
		if f.ID == id && (f.RecvBytesPerSec != 0 || f.State != "completed") {
			t.Fatal(f)
		}
	}
}

func TestCohortIdentitySeparatesProcessInstances(t *testing.T) {
	flow := Flow{Key: FlowKey{Protocol: protocolTCP, DestinationPort: 443}}
	a := cohortFor(flow, flowOwner{PID: 1, Instance: "1:100"})
	b := cohortFor(flow, flowOwner{PID: 1, Instance: "1:200"})
	if a == b || a == cohortFor(flow, flowOwner{}) {
		t.Fatal("reused PID merged cohorts")
	}
}

// Exercise the same upstream/bridge path used by the tunnel with four
// concurrent content sockets. This is a route-free loopback test, not proof of
// throughput across physical internet links.
func TestForwarderCohortCarriesConcurrentTransfers(t *testing.T) {
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	payload := bytes.Repeat([]byte("verified-download-chunk"), 8192)
	serverDone := make(chan error, 4)
	go func() {
		for range 4 {
			conn, e := server.Accept()
			if e != nil {
				serverDone <- e
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				_, e := c.Write(payload)
				serverDone <- e
			}(conn)
		}
	}()
	scheduler := NewScheduler([]Link{{Index: 1, Name: "a", Weight: 1, Available: true}, {Index: 2, Name: "b", Weight: 1, Available: true}})
	var mu sync.Mutex
	counts := map[uint32]int{}
	forwarder := newHardeningForwarder(t, scheduler, func(ctx context.Context, idx uint32, network, address string) (net.Conn, error) {
		mu.Lock()
		counts[idx]++
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, "tcp4", server.Addr().String())
	})
	defer forwarder.Close()
	_, port, _ := net.SplitHostPort(server.Addr().String())
	p, _ := strconv.Atoi(port)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			flow := Flow{Key: FlowKey{Protocol: protocolTCP, Source: netip.MustParseAddr("198.18.0.2"), SourcePort: uint16(55000 + i), Destination: netip.MustParseAddr("127.0.0.1"), DestinationPort: uint16(p)}}
			upstream, lease, e := forwarder.openUpstream(flow, "tcp")
			if e != nil {
				t.Error(e)
				return
			}
			app, client := net.Pipe()
			if !forwarder.beginFlow(protocolTCP) {
				t.Error("admission failed")
				return
			}
			done := make(chan struct{})
			go func() { defer close(done); forwarder.bridgeTCP(flow, app, upstream, lease) }()
			client.SetDeadline(time.Now().Add(5 * time.Second))
			body := make([]byte, len(payload))
			_, e = io.ReadFull(client, body)
			client.Close()
			<-done
			if e != nil || !bytes.Equal(body, payload) {
				t.Errorf("payload mismatch: %v", e)
			}
		}(i)
	}
	wg.Wait()
	for range 4 {
		if e := <-serverDone; e != nil {
			t.Error(e)
		}
	}
	if counts[1] != 2 || counts[2] != 2 {
		t.Fatal("content connections not spread", counts)
	}
	for _, link := range scheduler.Snapshots() {
		if link.ActiveConnections != 0 || link.BytesReceived != uint64(2*len(payload)) {
			t.Fatal("wrong link totals", link)
		}
	}
	diagnostics := forwarder.FlowSnapshots()
	if len(diagnostics) != 4 {
		t.Fatal("missing correlated transfers", len(diagnostics))
	}
	for _, f := range diagnostics {
		if f.State != "completed" || f.BytesReceived != uint64(len(payload)) || f.UpstreamLocal == "" {
			t.Fatal(f)
		}
	}
}
