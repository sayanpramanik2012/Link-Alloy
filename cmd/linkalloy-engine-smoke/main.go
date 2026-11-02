// linkalloy-engine-smoke runs the Windows engine directly, without Wails or an installer.
// It has a hard two-minute cap and always asks the controller to restore routes on exit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sayanpramanik2012/Link-Alloy/internal/engine"
)

const maxDuration = 2 * time.Minute

func main() {
	if err := run(); err != nil {
		fatal("engine smoke failed: %v", err)
	}
}

func run() (resultErr error) {
	duration := flag.Duration("duration", 30*time.Second, "automatic stop time (15s to 2m)")
	recoverOnly := flag.Bool("recover-only", false, "restore a saved tunnel cleanup record without starting the tunnel")
	flag.Parse()
	release, lockErr := engine.AcquireProcessLock()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	if *recoverOnly {
		controller := engine.NewTunnelController(nil)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := controller.RecoverStale(ctx); err != nil {
			return fmt.Errorf("stale tunnel recovery failed: %w", err)
		}
		fmt.Println("Saved tunnel cleanup record processed.")
		return nil
	}
	if *duration < 15*time.Second || *duration > maxDuration {
		return errors.New("duration must be between 15 seconds and 2 minutes")
	}
	lookupCtx, cancelLookup := context.WithTimeout(context.Background(), 8*time.Second)
	ipAddresses, lookupErr := net.DefaultResolver.LookupIP(lookupCtx, "ip4", "api.ipify.org")
	cancelLookup()
	if lookupErr != nil {
		return fmt.Errorf("resolve the HTTPS smoke-test endpoint before enabling the tunnel: %w", lookupErr)
	}
	if len(ipAddresses) == 0 {
		return errors.New("the HTTPS smoke-test endpoint returned no IPv4 addresses")
	}
	targetIPv4 := ipAddresses[0]

	controller := engine.NewTunnelController(nil)
	startup, cancelStartup := context.WithTimeout(context.Background(), 45*time.Second)
	startErr := controller.Start(startup)
	cancelStartup()
	if startErr != nil {
		return fmt.Errorf("tunnel start failed: %w", startErr)
	}
	defer func() {
		if err := controller.Stop(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("tunnel cleanup failed: %w", err))
		}
	}()
	fmt.Printf("Tunnel is active. It will stop automatically after %s; press Ctrl+C to stop sooner.\n", *duration)
	status := controller.Status()
	printStatus(status)
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 12*time.Second)
	paths, probeErr := capturedHTTPSProbe(probeCtx, targetIPv4)
	cancelProbe()
	if probeErr != nil {
		return fmt.Errorf("captured HTTPS smoke request failed: %w", probeErr)
	}
	healthyLinks := 0
	for _, link := range status.Links {
		if link.Available {
			healthyLinks++
		}
	}
	if healthyLinks > 1 && paths < 2 {
		return fmt.Errorf("captured HTTPS requests used %d egress path(s) with %d healthy links", paths, healthyLinks)
	}
	fmt.Printf("Captured HTTPS requests passed through %d distinct egress path(s).\n", paths)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	timer := time.NewTimer(*duration)
	ticker := time.NewTicker(3 * time.Second)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			fmt.Println("Automatic test window ended; restoring Windows routes.")
			goto stop
		case <-ctx.Done():
			fmt.Println("Stop requested; restoring Windows routes.")
			goto stop
		case <-ticker.C:
			printStatus(controller.Status())
		}
	}

stop:
	if err := controller.Stop(); err != nil {
		return fmt.Errorf("tunnel cleanup failed: %w", err)
	}
	finalStatus := controller.Status()
	if finalStatus.State != "stopped" {
		return fmt.Errorf("tunnel ended in state %q: %s", finalStatus.State, finalStatus.LastError)
	}
	fmt.Println("Tunnel stopped and Windows route cleanup verified.")
	if logPath, err := controller.LogFilePath(); err == nil {
		fmt.Println("Engine log:", logPath)
	}
	return nil
}

func capturedHTTPSProbe(ctx context.Context, targetIPv4 net.IP) (int, error) {
	type probeResult struct {
		address string
		err     error
	}
	const requests = 8
	results := make(chan probeResult, requests)
	for range requests {
		go func() {
			dialer := &net.Dialer{Timeout: 8 * time.Second}
			transport := &http.Transport{
				Proxy: nil, ForceAttemptHTTP2: false, DisableKeepAlives: true,
				DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
					address := net.JoinHostPort(targetIPv4.String(), "443")
					return dialer.DialContext(dialCtx, "tcp4", address)
				},
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
			if err != nil {
				results <- probeResult{err: err}
				return
			}
			response, err := client.Do(request)
			if err != nil {
				results <- probeResult{err: err}
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				results <- probeResult{err: fmt.Errorf("unexpected status %s", response.Status)}
				return
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 128))
			if err != nil {
				results <- probeResult{err: err}
				return
			}
			ip := net.ParseIP(strings.TrimSpace(string(body)))
			if ip == nil {
				results <- probeResult{err: errors.New("egress check returned an invalid response")}
				return
			}
			results <- probeResult{address: ip.String()}
		}()
	}
	addresses := make(map[string]struct{}, requests)
	for range requests {
		select {
		case result := <-results:
			if result.err != nil {
				return len(addresses), result.err
			}
			addresses[result.address] = struct{}{}
		case <-ctx.Done():
			return len(addresses), ctx.Err()
		}
	}
	return len(addresses), nil
}

func printStatus(status engine.RuntimeStatus) {
	for _, link := range status.Links {
		fmt.Printf("%s: healthy=%t active=%d download=%.2f MB/s upload=%.2f MB/s received=%d sent=%d\n",
			link.Name, link.Available, link.ActiveConnections,
			link.RecvBytesPerSec/1_000_000, link.SendBytesPerSec/1_000_000,
			link.BytesReceived, link.BytesSent)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
