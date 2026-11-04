package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestInterleavedServicesDoNotConcentrateContent(t *testing.T) {
	for _, n := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			links := make([]Link, n)
			for i := range links {
				links[i] = Link{Index: uint32(i + 1), Name: fmt.Sprint(i + 1), Weight: 4, Available: true}
			}
			s := NewScheduler(links)
			owner := flowOwner{PID: 42, Instance: "42:creation", Name: "launcher.exe"}
			flow := Flow{Key: FlowKey{Protocol: protocolTCP, DestinationPort: 443, Destination: netip.MustParseAddr("203.0.113.10")}}
			counts := map[uint32]int{}
			for range n * 4 {
				content, e := s.acquireCohort(cohortFor(flow, owner, "steamcontent.com"), nil, nil)
				if e != nil {
					t.Fatal(e)
				}
				counts[content.LinkIndex()]++
				content.Release()
				// Same executable, same port: a login/control connection between content
				// opens must not consume the content pool's next scheduling position.
				for range n - 1 {
					background, e := s.acquireCohort(cohortFor(flow, owner, "steampowered.com"), nil, nil)
					if e != nil {
						t.Fatal(e)
					}
					background.Release()
				}
			}
			for _, l := range links {
				if counts[l.Index] != 4 {
					t.Fatalf("content distribution=%v; expected four per link", counts)
				}
			}
		})
	}
}

func TestServiceDomainUsesPublicSuffixRules(t *testing.T) {
	for host, want := range map[string]string{
		"cache1-maa2.steamcontent.com":  "steamcontent.com",
		"CACHE2-SGP1.STEAMCONTENT.COM.": "steamcontent.com",
		"login.steampowered.com":        "steampowered.com",
		"cdn.download.example.co.uk":    "example.co.uk",
		"localhost":                     "", "127.0.0.1": "", "bad/name.com": "", "-bad.example.com": "",
	} {
		if got := serviceDomain(host); got != want {
			t.Errorf("%s: got %q, want %q", host, got, want)
		}
	}
}

func TestIPv6HealthChangesPreserveIPv4ServiceRotation(t *testing.T) {
	s := NewScheduler([]Link{{Index: 1, Name: "a", Weight: 4, Available: true, FamiliesKnown: true, IPv4Available: true}, {Index: 2, Name: "b", Weight: 4, Available: true, FamiliesKnown: true, IPv4Available: true}})
	ipv6 := false
	counts := map[uint32]int{}
	for i := 0; i < 20; i++ {
		l, e := s.acquireCohort("content", nil, &ipv6)
		if e != nil {
			t.Fatal(e)
		}
		counts[l.LinkIndex()]++
		l.Release()
		bg, e := s.acquireCohort(fmt.Sprint("background", i), nil, &ipv6)
		if e != nil {
			t.Fatal(e)
		}
		bg.Release()
		s.SetHealth(2, true, i%2 == 0, "healthy", "")
	}
	if counts[1] != 10 || counts[2] != 10 {
		t.Fatal("IPv6 probes biased IPv4 assignment", counts)
	}
}

func TestTLSInspectionFallbackPreservesEveryByte(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	payload := []byte("HELLO this is a non-TLS protocol on port 443")
	go func() { defer client.Close(); client.Write(payload) }()
	prefix, service := inspectTLSService(server)
	rest, err := io.ReadAll(server)
	if err != nil || service != "" || !bytes.Equal(append(prefix, rest...), payload) {
		t.Fatalf("fallback changed bytes: service=%q, err=%v", service, err)
	}
}

func TestTLSInspectionServerFirstTimeoutIsBounded(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	start := time.Now()
	prefix, service := inspectTLSService(server)
	if len(prefix) != 0 || service != "" || time.Since(start) > 2*time.Second {
		t.Fatal("server-first fallback was not bounded")
	}
	// Inspection must not close the real connection or leave its deadline set.
	go func() { client.Write([]byte("ok")) }()
	server.SetReadDeadline(time.Now().Add(time.Second))
	data := make([]byte, 2)
	if _, err := io.ReadFull(server, data); err != nil || string(data) != "ok" {
		t.Fatal("connection was not reusable", err)
	}
}

func TestServiceRoutingPreservesRealTLSAndPayload(t *testing.T) {
	payload := bytes.Repeat([]byte("unchanged encrypted transfer"), 8192)
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer target.Close()
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	host := target.Certificate().DNSNames[0]
	scheduler := NewScheduler([]Link{{Index: 1, Name: "test", Weight: 4, Available: true}})
	forwarder := newHardeningForwarder(t, scheduler, func(ctx context.Context, idx uint32, network, address string) (net.Conn, error) {
		if address != "203.0.113.10:443" {
			t.Error("original target changed", address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Listener.Addr().String())
	})
	defer forwarder.Close()
	app, client := net.Pipe()
	flow := Flow{Key: FlowKey{Protocol: protocolTCP, Source: netip.MustParseAddr("198.18.0.2"), SourcePort: 55000, Destination: netip.MustParseAddr("203.0.113.10"), DestinationPort: 443}}
	if !forwarder.beginFlow(protocolTCP) {
		t.Fatal("admission failed")
	}
	done := make(chan struct{})
	go func() { defer close(done); forwarder.connectTCP(flow, app, flowOwner{PID: 42, Instance: "42:test"}) }()
	secure := tls.Client(client, &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12})
	secure.SetDeadline(time.Now().Add(5 * time.Second))
	if err := secure.Handshake(); err != nil {
		client.Close()
		t.Fatal("end-to-end TLS handshake failed", err)
	}
	if _, err := secure.Write([]byte("GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n")); err != nil {
		client.Close()
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(secure), &http.Request{Method: "GET"})
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	secure.Close()
	client.Close()
	if err != nil || !bytes.Equal(body, payload) {
		t.Fatal("TLS payload changed", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TLS worker did not stop")
	}
	views := forwarder.FlowSnapshots()
	if len(views) != 1 || views[0].ServiceName != serviceDomain(host) || views[0].BytesReceived < uint64(len(payload)) {
		t.Fatal("missing service diagnostics", views)
	}
}

func TestPersistentHTTP2ServicesKeepBothLinksAcrossRequestRounds(t *testing.T) {
	payload := bytes.Repeat([]byte("persistent-h2-data"), 4096)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("expected HTTP/2", r.Proto)
		}
		w.Write(payload)
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	s := NewScheduler([]Link{{Index: 1, Name: "a", Weight: 4, Available: true}, {Index: 2, Name: "b", Weight: 4, Available: true}})
	var mu sync.Mutex
	counts := map[uint32]int{}
	f := newHardeningForwarder(t, s, func(ctx context.Context, index uint32, network, address string) (net.Conn, error) {
		mu.Lock()
		counts[index]++
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Listener.Addr().String())
	})
	defer f.Close()
	var port uint16 = 55000
	transport := &http.Transport{ForceAttemptHTTP2: true, MaxConnsPerHost: 1, TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: target.Certificate().DNSNames[0]}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		app, client := net.Pipe()
		if !f.beginFlow(protocolTCP) {
			app.Close()
			client.Close()
			return nil, ErrFlowCapacity
		}
		mu.Lock()
		p := port
		port++
		mu.Unlock()
		flow := Flow{Key: FlowKey{Protocol: protocolTCP, Source: netip.MustParseAddr("198.18.0.2"), SourcePort: p, Destination: netip.MustParseAddr("203.0.113.10"), DestinationPort: 443}}
		go f.connectTCP(flow, app, flowOwner{PID: 42, Instance: "42:persistent"})
		return client, nil
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		for node := 0; node < 4; node++ {
			wg.Add(1)
			go func(node int) {
				defer wg.Done()
				response, err := client.Get(fmt.Sprintf("https://node%d.invalid/chunk/%d", node, round))
				if err != nil {
					t.Error(err)
					return
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || !bytes.Equal(body, payload) {
					t.Error("persistent transfer changed data", err)
				}
			}(node)
		}
		wg.Wait()
	}
	mu.Lock()
	a, b := counts[1], counts[2]
	mu.Unlock()
	if a != 2 || b != 2 {
		t.Fatalf("persistent content sockets=%d/%d, expected 2/2", a, b)
	}
	views := f.FlowSnapshots()
	if len(views) != 4 {
		t.Fatal("HTTP/2 connection reuse was lost", len(views))
	}
	for _, v := range views {
		if v.BytesReceived < uint64(8*len(payload)) {
			t.Fatal("persistent socket lost payload", v)
		}
	}
}

func TestShutdownDuringTLSInspectionReleasesAdmission(t *testing.T) {
	f := newHardeningForwarder(t, NewScheduler([]Link{{Index: 1, Name: "test", Weight: 1, Available: true}}), func(context.Context, uint32, string, string) (net.Conn, error) { return nil, context.Canceled })
	app, client := net.Pipe()
	defer client.Close()
	if !f.beginFlow(protocolTCP) {
		t.Fatal("admission failed")
	}
	flow := Flow{Key: FlowKey{Protocol: protocolTCP, Destination: netip.MustParseAddr("203.0.113.10"), DestinationPort: 443}}
	go f.connectTCP(flow, app, flowOwner{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	if f.Stats().ActiveTCP != 0 {
		t.Fatal("shutdown leaked an inspecting connection")
	}
}

type helloTestConn struct {
	*bytes.Reader
	writes, closes int
}

func (c *helloTestConn) Write(p []byte) (int, error)    { c.writes++; return len(p), nil }
func (c *helloTestConn) Close() error                   { c.closes++; return nil }
func (*helloTestConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*helloTestConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*helloTestConn) SetDeadline(time.Time) error      { return nil }
func (*helloTestConn) SetReadDeadline(time.Time) error  { return nil }
func (*helloTestConn) SetWriteDeadline(time.Time) error { return nil }

func FuzzTLSServiceInspection(f *testing.F) {
	f.Add([]byte("not a TLS stream"))
	f.Add([]byte{22, 3, 3, 0, 5, 1, 0, 0, 1, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxTLSInspectionBytes {
			data = data[:maxTLSInspectionBytes]
		}
		conn := &helloTestConn{Reader: bytes.NewReader(data)}
		prefix, _ := inspectTLSService(conn)
		if conn.writes != 0 || conn.closes != 0 {
			t.Fatal("inspection changed the real connection")
		}
		rest, err := io.ReadAll(conn)
		if err != nil || !bytes.Equal(append(prefix, rest...), data) {
			t.Fatal("inspection changed or lost bytes")
		}
	})
}
