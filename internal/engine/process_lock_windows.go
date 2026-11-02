//go:build windows

package engine

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const linkAlloyProcessMutexName = `Global\LinkAlloy-4F6A9D10-2B57-4F31-A82D-6C696E6B616C`

// Every interactive user may coordinate on this mutex. It grants no route,
// state-file, or process-control permission.
const processMutexSDDL = "D:P(A;;0x001f0001;;;SY)(A;;0x001f0001;;;BA)(A;;0x001f0001;;;AU)"

var ErrProcessAlreadyRunning = errors.New("another Link Alloy process owns the network-session lock")

// AcquireProcessLock owns a named global mutex until the returned release
// function is called. A dedicated locked OS thread owns the Windows mutex for
// its full lifetime, because Windows mutex ownership is thread-affine.
func AcquireProcessLock() (func(), error) {
	return acquireProcessLock(linkAlloyProcessMutexName)
}

func acquireProcessLock(name string) (func(), error) {
	name16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(processMutexSDDL)
	if err != nil {
		return nil, fmt.Errorf("create process-lock ACL: %w", err)
	}
	security := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}
	ready := make(chan error, 1)
	releaseSignal := make(chan struct{})
	released := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(released)

		handle, createErr := windows.CreateMutexEx(security, name16, 0, windows.MUTEX_MODIFY_STATE|windows.SYNCHRONIZE)
		if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
			createErr = nil
		}
		if createErr != nil || handle == 0 {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
			if createErr == nil {
				createErr = errors.New("CreateMutex returned an invalid handle")
			}
			ready <- createErr
			return
		}

		result, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil {
			_ = windows.CloseHandle(handle)
			ready <- fmt.Errorf("wait for process lock: %w", waitErr)
			return
		}
		if result == uint32(windows.WAIT_TIMEOUT) {
			_ = windows.CloseHandle(handle)
			ready <- ErrProcessAlreadyRunning
			return
		}
		if result != uint32(windows.WAIT_OBJECT_0) && result != uint32(windows.WAIT_ABANDONED) {
			_ = windows.CloseHandle(handle)
			ready <- fmt.Errorf("unexpected process-lock wait result 0x%x", result)
			return
		}
		ready <- nil
		<-releaseSignal
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
	}()

	if err := <-ready; err != nil {
		<-released
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { close(releaseSignal) })
		<-released
	}, nil
}
