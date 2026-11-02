//go:build windows

package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

const (
	ipUnicastIf       = 31
	ipv6UnicastIf     = 31
	ipv6ProtocolLevel = 41
)

// NewLinkDialer pins each socket to one Windows interface. This is for Link
// Alloy's own upstream sockets; it does not capture application traffic.
func NewLinkDialer(interfaceIndex uint32, timeout time.Duration) (*net.Dialer, error) {
	if interfaceIndex == 0 {
		return nil, errors.New("interface index must be non-zero")
	}
	return &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, raw syscall.RawConn) error {
			var optionErr error
			ipv6, familyErr := isIPv6Socket(network, address)
			if familyErr != nil {
				return familyErr
			}
			if err := raw.Control(func(fd uintptr) {
				if !ipv6 {
					// Windows expects the IPv4 interface index in network byte order.
					value := bits.ReverseBytes32(interfaceIndex)
					optionErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, ipUnicastIf, int(value))
				} else {
					optionErr = syscall.SetsockoptInt(syscall.Handle(fd), ipv6ProtocolLevel, ipv6UnicastIf, int(interfaceIndex))
				}
			}); err != nil {
				return err
			}
			return optionErr
		},
	}, nil
}

func isIPv6Socket(network, address string) (bool, error) {
	if strings.HasSuffix(network, "6") {
		return true, nil
	}
	if strings.HasSuffix(network, "4") {
		return false, nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false, fmt.Errorf("determine socket address family: %w", err)
	}
	host = strings.SplitN(host, "%", 2)[0]
	ip := net.ParseIP(host)
	if ip == nil {
		return false, fmt.Errorf("cannot determine socket address family for %q", address)
	}
	return ip.To4() == nil, nil
}

// ProbePublicIP performs a small, proxy-free request over the selected link.
// It verifies interface binding without changing Windows routes or settings.
func ProbePublicIP(ctx context.Context, interfaceIndex uint32) (string, error) {
	dialer, err := NewLinkDialer(interfaceIndex, 8*time.Second)
	if err != nil {
		return "", err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		ForceAttemptHTTP2:     false,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("egress probe returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 128))
	if err != nil {
		return "", err
	}
	address := strings.TrimSpace(string(body))
	if net.ParseIP(address) == nil {
		return "", errors.New("egress probe returned an invalid IP address")
	}
	return address, nil
}

// ProbeLinkFamily verifies new-connection eligibility separately for each IP family.
// Independent services prevent one endpoint outage from declaring every link down.
var ErrIPFamilyUnavailable = errors.New("interface has no usable address for this IP family")

func ProbeLinkFamily(ctx context.Context, index uint32, ipv6 bool) error {
	iface, err := net.InterfaceByIndex(int(index))
	if err != nil {
		return err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return err
	}
	usable := false
	for _, a := range addrs {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil && ip.IsGlobalUnicast() && ((ip.To4() == nil) == ipv6) {
			usable = true
			break
		}
	}
	if !usable {
		return ErrIPFamilyUnavailable
	}
	network := "tcp4"
	endpoints := []string{"https://api4.ipify.org", "https://ipv4.icanhazip.com"}
	if ipv6 {
		network = "tcp6"
		endpoints = []string{"https://api6.ipify.org", "https://ipv6.icanhazip.com"}
	}
	dialer, err := NewLinkDialer(index, 3*time.Second)
	if err != nil {
		return err
	}
	for _, url := range endpoints {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attempt, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
		transport := &http.Transport{Proxy: nil, DialContext: func(c context.Context, _, a string) (net.Conn, error) { return dialer.DialContext(c, network, a) }, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 3 * time.Second, DisableKeepAlives: true}
		client := http.Client{Transport: transport}
		req, e := http.NewRequestWithContext(attempt, http.MethodGet, url, nil)
		if e == nil {
			var response *http.Response
			response, e = client.Do(req)
			if e == nil {
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 128))
				response.Body.Close()
				ip := net.ParseIP(strings.TrimSpace(string(body)))
				if readErr == nil && response.StatusCode == 200 && ip != nil && ((ip.To4() == nil) == ipv6) {
					transport.CloseIdleConnections()
					cancel()
					return nil
				}
			}
		}
		transport.CloseIdleConnections()
		cancel()
	}
	return errors.New("connectivity probe did not verify this IP family")
}
