//go:build windows

package engine

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func requireAdministrator() error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("check administrator access: %w", err)
	}
	defer token.Close()
	if !token.IsElevated() {
		return ErrElevationRequired
	}
	return nil
}
