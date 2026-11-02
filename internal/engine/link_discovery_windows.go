//go:build windows

package engine

import (
	"errors"
	"golang.org/x/sys/windows"
	"net"
	"strings"
	"unsafe"
)

// UpstreamInterfaces lists physical Ethernet, Wi-Fi and mobile interfaces.
func UpstreamInterfaces() ([]net.Interface, error) {
	var size uint32
	err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, nil, &size)
	if err != nil && !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	b := make([]byte, size)
	a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&b[0]))
	if err = windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, a, &size); err != nil {
		return nil, err
	}
	allowed := map[int]bool{}
	for p := a; p != nil; p = p.Next {
		if p.IfType != 6 && p.IfType != 71 && p.IfType != 243 && p.IfType != 244 {
			continue
		}
		desc := strings.ToLower(windows.UTF16PtrToString(p.Description))
		virtual := false
		for _, word := range []string{"virtual", "hyper-v", "wintun", "tap-windows", "vpn", "wi-fi direct"} {
			if strings.Contains(desc, word) {
				virtual = true
				break
			}
		}
		if !virtual {
			allowed[int(p.IfIndex)] = true
		}
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []net.Interface{}
	for _, i := range ifs {
		if allowed[i.Index] && i.Flags&net.FlagLoopback == 0 {
			out = append(out, i)
		}
	}
	return out, nil
}
