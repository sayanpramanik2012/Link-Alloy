//go:build windows

package engine

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestProcessLockIsExclusiveAndReacquirable(t *testing.T) {
	name := fmt.Sprintf(`Local\LinkAlloyTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	release, err := acquireProcessLock(name)
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, err := acquireProcessLock(name)
	if err == nil {
		secondRelease()
		release()
		t.Fatal("second process-lock acquisition unexpectedly succeeded")
	}
	if !errors.Is(err, ErrProcessAlreadyRunning) {
		release()
		t.Fatalf("second process-lock error = %v, want ErrProcessAlreadyRunning", err)
	}
	release()

	thirdRelease, err := acquireProcessLock(name)
	if err != nil {
		t.Fatalf("process lock was not released: %v", err)
	}
	thirdRelease()
}
