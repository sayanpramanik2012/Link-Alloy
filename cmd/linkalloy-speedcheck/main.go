// linkalloy-speedcheck measures bounded HTTPS payload transfers. It never
// creates a tunnel, installs routes, changes link settings, or writes payloads.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sayanpramanik2012/Link-Alloy/internal/engine"
)

type sample struct {
	Interface  uint32    `json:"interface"`
	Name       string    `json:"name"`
	Started    time.Time `json:"started"`
	Seconds    float64   `json:"seconds"`
	Bytes      uint64    `json:"bytes"`
	LimitBytes int64     `json:"limitBytes"`
	Mbps       float64   `json:"mbps"`
	Sources    []string  `json:"localSources"`
	Errors     []string  `json:"errors,omitempty"`
}
type sink struct {
	total  *atomic.Uint64
	limit  uint64
	cancel context.CancelFunc
}

func (s sink) Write(p []byte) (int, error) {
	for {
		old := s.total.Load()
		if old >= s.limit {
			s.cancel()
			return 0, context.Canceled
		}
		n := len(p)
		if uint64(n) > s.limit-old {
			n = int(s.limit - old)
		}
		if !s.total.CompareAndSwap(old, old+uint64(n)) {
			continue
		}
		if old+uint64(n) >= s.limit {
			s.cancel()
			return n, context.Canceled
		}
		return n, nil
	}
}

func main() {
	indices := flag.String("interfaces", "", "Comma-separated physical interface indices; 0 uses the current system route")
	limit := flag.Int64("bytes", 128<<20, "Maximum aggregate payload bytes per sample")
	duration := flag.Duration("duration", 10*time.Second, "Maximum wall time per sample")
	streams := flag.Int("streams", 4, "Concurrent HTTPS connections")
	flag.Parse()
	if *indices == "" || *limit < 1<<20 || *limit > 256<<20 || *duration < time.Second || *duration > 20*time.Second || *streams < 1 || *streams > 8 {
		fmt.Fprintln(os.Stderr, "Provide interfaces, 1-256 MiB, 1-20s, and 1-8 streams")
		os.Exit(2)
	}
	for _, value := range strings.Split(*indices, ",") {
		idx, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		result := measure(uint32(idx), *limit, *duration, *streams)
		json.NewEncoder(os.Stdout).Encode(result)
	}
}

func measure(index uint32, limit int64, duration time.Duration, streams int) sample {
	result := sample{Interface: index, Name: "System route", LimitBytes: limit, Started: time.Now()}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if index != 0 {
		iface, err := net.InterfaceByIndex(int(index))
		if err != nil {
			result.Errors = []string{err.Error()}
			return result
		}
		result.Name = iface.Name
		dialer, err = engine.NewLinkDialer(index, 5*time.Second)
		if err != nil {
			result.Errors = []string{err.Error()}
			return result
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	var mu sync.Mutex
	sources := map[string]bool{}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, ForceAttemptHTTP2: false, MaxConnsPerHost: streams, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, "tcp4", address)
		if err == nil {
			host, _, _ := net.SplitHostPort(conn.LocalAddr().String())
			mu.Lock()
			sources[host] = true
			mu.Unlock()
		}
		return conn, err
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var total atomic.Uint64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil && total.Load() < uint64(limit) {
				want := int64(32 << 20)
				if limit < want {
					want = limit
				}
				err := download(ctx, client, want, sink{total: &total, limit: uint64(limit), cancel: cancel})
				if err != nil {
					if ctx.Err() == nil {
						mu.Lock()
						result.Errors = append(result.Errors, err.Error())
						mu.Unlock()
					}
					return
				}
			}
		}()
	}
	wg.Wait()
	result.Seconds = time.Since(start).Seconds()
	result.Bytes = total.Load()
	result.Mbps = float64(result.Bytes) * 8 / result.Seconds / 1e6
	for source := range sources {
		result.Sources = append(result.Sources, source)
	}
	return result
}

func download(ctx context.Context, client *http.Client, want int64, output sink) error {
	url := fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", want)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Link-Alloy-Diagnostics")
	req.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	n, err := io.Copy(output, io.LimitReader(response.Body, want))
	if err == nil && n != want {
		return io.ErrUnexpectedEOF
	}
	return err
}
