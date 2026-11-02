//go:build windows

package engine

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

// The final handoff is a single EXE. The verified runtime is extracted beside
// it on first use because the Wintun loader resolves its DLL from the
// application directory.
//
//go:embed runtime/amd64/wintun.dll
var embeddedWintunAMD64 []byte

//go:embed runtime/arm64/wintun.dll
var embeddedWintunARM64 []byte

const (
	linkAlloyTunnelName = "Link Alloy Tunnel"
	linkAlloyTunnelType = "LinkAlloy"
	wintunRingCapacity  = 0x800000
)

var linkAlloyTunnelGUID = windows.GUID{
	Data1: 0x4f6a9d10,
	Data2: 0x2b57,
	Data3: 0x4f31,
	Data4: [8]byte{0xa8, 0x2d, 0x6c, 0x69, 0x6e, 0x6b, 0x61, 0x6c},
}

type wintunDevice struct {
	adapter   *wintun.Adapter
	session   wintun.Session
	readEvent windows.Handle
	name      string
	luid      uint64
	ipv4Index uint32
	ipv6Index uint32
	closeOnce sync.Once
}

func NewTunnelDevice(name string) (TunnelDevice, error) {
	if err := requireAdministrator(); err != nil {
		return nil, err
	}
	if err := verifyWintunRuntime(); err != nil {
		return nil, err
	}
	if name == "" {
		name = linkAlloyTunnelName
	}
	adapter, err := wintun.CreateAdapter(name, linkAlloyTunnelType, &linkAlloyTunnelGUID)
	if err != nil {
		return nil, fmt.Errorf("create Wintun adapter: %w", err)
	}
	session, err := adapter.StartSession(wintunRingCapacity)
	if err != nil {
		_ = adapter.Close()
		return nil, fmt.Errorf("start Wintun session: %w", err)
	}
	device := &wintunDevice{
		adapter:   adapter,
		session:   session,
		readEvent: session.ReadWaitEvent(),
		name:      name,
		luid:      adapter.LUID(),
	}
	for attempt := 0; attempt < 30; attempt++ {
		device.ipv4Index, device.ipv6Index, err = findInterfaceIndexes(device.luid)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		_ = device.Close()
		return nil, err
	}
	return device, nil
}

func verifyWintunRuntime() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate Link Alloy executable: %w", err)
	}
	dllPath := filepath.Join(filepath.Dir(executable), "wintun.dll")
	if err := ensureEmbeddedWintunRuntime(dllPath); err != nil {
		return err
	}
	data, err := os.ReadFile(dllPath)
	if err != nil {
		return fmt.Errorf("the signed Wintun runtime is missing beside Link Alloy: %w", err)
	}
	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	want := ""
	switch runtime.GOARCH {
	case "amd64":
		want = "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"
	case "arm64":
		want = "f7ba89005544be9d85231a9e0d5f23b2d15b3311667e2dad0debd344918a3f80"
	default:
		return fmt.Errorf("Wintun is not packaged for Windows %s", runtime.GOARCH)
	}
	if actual != want {
		return errors.New("the Wintun runtime beside Link Alloy does not match the verified official binary")
	}
	return nil
}

func ensureEmbeddedWintunRuntime(path string) error {
	runtimeBytes := embeddedWintunAMD64
	want := "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"
	if runtime.GOARCH == "arm64" {
		runtimeBytes = embeddedWintunARM64
		want = "f7ba89005544be9d85231a9e0d5f23b2d15b3311667e2dad0debd344918a3f80"
	}
	digest := sha256.Sum256(runtimeBytes)
	if hex.EncodeToString(digest[:]) != want {
		return errors.New("embedded Wintun runtime failed its built-in hash check")
	}
	if data, err := os.ReadFile(path); err == nil {
		current := sha256.Sum256(data)
		if hex.EncodeToString(current[:]) == want {
			return nil
		}
	}
	tmp := path + ".linkalloy.tmp"
	if err := os.WriteFile(tmp, runtimeBytes, 0600); err != nil {
		return fmt.Errorf("extract embedded Wintun runtime beside Link Alloy: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activate embedded Wintun runtime: %w", err)
	}
	return nil
}

func (d *wintunDevice) InterfaceName() string { return d.name }

func (d *wintunDevice) InterfaceLUID() uint64 { return d.luid }

func (d *wintunDevice) InterfaceIndexes() (ipv4, ipv6 uint32) {
	return d.ipv4Index, d.ipv6Index
}

func (d *wintunDevice) Receive(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		packet, err := d.session.ReceivePacket()
		if err == nil {
			copyOfPacket := append([]byte(nil), packet...)
			d.session.ReleaseReceivePacket(packet)
			return copyOfPacket, nil
		}
		if !errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			if errors.Is(err, windows.ERROR_HANDLE_EOF) {
				return nil, errors.New("Wintun session ended")
			}
			return nil, fmt.Errorf("receive Wintun packet: %w", err)
		}
		result, waitErr := windows.WaitForSingleObject(d.readEvent, 200)
		if waitErr != nil {
			return nil, fmt.Errorf("wait for Wintun packet: %w", waitErr)
		}
		if result == windows.WAIT_FAILED {
			return nil, fmt.Errorf("wait for Wintun packet: %w", syscall.GetLastError())
		}
	}
}

func (d *wintunDevice) Send(packet []byte) error {
	if len(packet) == 0 || len(packet) > wintun.PacketSizeMax {
		return fmt.Errorf("invalid IP packet length %d", len(packet))
	}
	for attempt := 0; attempt < 3; attempt++ {
		buffer, err := d.session.AllocateSendPacket(len(packet))
		if err == nil {
			copy(buffer, packet)
			d.session.SendPacket(buffer)
			return nil
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return fmt.Errorf("allocate Wintun send buffer: %w", err)
		}
		if attempt < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	return ErrTunnelBackpressure
}

func (d *wintunDevice) Close() error {
	var closeErr error
	d.closeOnce.Do(func() {
		d.session.End()
		closeErr = d.adapter.Close()
	})
	return closeErr
}

func findInterfaceIndexes(luid uint64) (ipv4Index, ipv6Index uint32, err error) {
	var size uint32
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, nil, &size); err != nil && !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
		return 0, 0, fmt.Errorf("query Windows adapters: %w", err)
	}
	if size == 0 {
		return 0, 0, errors.New("Windows returned an empty adapter list")
	}
	buffer := make([]byte, size)
	addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, addresses, &size); err != nil {
		return 0, 0, fmt.Errorf("enumerate Windows adapters: %w", err)
	}
	for current := addresses; current != nil; current = current.Next {
		if current.Luid == luid {
			return current.IfIndex, current.Ipv6IfIndex, nil
		}
	}
	return 0, 0, errors.New("created Wintun adapter is not present in the Windows interface table")
}

var _ TunnelDevice = (*wintunDevice)(nil)
