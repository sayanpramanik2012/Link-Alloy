//go:build windows

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const recoveryWatchArgument = "--linkalloy-recover-watch"

// StartRecoveryWatchdog starts an elevated child which waits for this process.
// It does no work while the parent owns the process lock. After an unexpected
// parent exit it acquires the lock, then restores only a validated journal.
func StartRecoveryWatchdog() (func(), error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate Link Alloy executable: %w", err)
	}
	cmd := exec.Command(executable, recoveryWatchArgument, strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start recovery watchdog: %w", err)
	}
	stopped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(stopped) }()
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
		}
	}, nil
}

// RunRecoveryWatchdog is dispatched before normal GUI startup. It waits for
// the parent process and never cleans a route journal while another Link Alloy
// process owns the global session lock.
func RunRecoveryWatchdog(parentPID uint32) error {
	if parentPID == 0 {
		return errors.New("recovery watchdog requires a parent process identifier")
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, parentPID)
	if err != nil && !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return fmt.Errorf("open Link Alloy parent process: %w", err)
	}
	if err == nil {
		defer windows.CloseHandle(parent)
		result, waitErr := windows.WaitForSingleObject(parent, windows.INFINITE)
		if waitErr != nil || result != uint32(windows.WAIT_OBJECT_0) {
			return fmt.Errorf("wait for Link Alloy parent process: %w", waitErr)
		}
	}
	release, err := AcquireProcessLock()
	if errors.Is(err, ErrProcessAlreadyRunning) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return NewRouteManager().Recover(ctx)
}

func IsRecoveryWatchdogArgument(args []string) (uint32, bool, error) {
	if len(args) != 3 || args[1] != recoveryWatchArgument {
		return 0, false, nil
	}
	pid, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil || pid == 0 {
		return 0, true, errors.New("invalid recovery watchdog parent process identifier")
	}
	return uint32(pid), true, nil
}
