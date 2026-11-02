//go:build windows

package engine

import "testing"

func TestRecoveryWatchdogArgumentValidation(t *testing.T) {
	pid, watch, err := IsRecoveryWatchdogArgument([]string{"linkalloy.exe", recoveryWatchArgument, "423"})
	if err != nil || !watch || pid != 423 {
		t.Fatalf("valid watchdog argument = (%d,%t,%v)", pid, watch, err)
	}
	if _, watch, err := IsRecoveryWatchdogArgument([]string{"linkalloy.exe"}); err != nil || watch {
		t.Fatal("ordinary application start was treated as a watchdog")
	}
	if _, watch, err := IsRecoveryWatchdogArgument([]string{"linkalloy.exe", recoveryWatchArgument, "not-a-pid"}); !watch || err == nil {
		t.Fatal("invalid watchdog argument was accepted")
	}
}
