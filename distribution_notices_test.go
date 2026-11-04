package main

import (
	"strings"
	"testing"
)

func TestPortableIncludesDistributionNotices(t *testing.T) {
	text := (&NetworkService{}).GetDistributionNotices()
	for _, name := range []string{"LICENSE-gVisor.txt", "LICENSE-React.txt", "LICENSE-Wails.txt", "LICENSE-Wintun-Prebuilt.txt", "THIRD_PARTY_NOTICES.md"} {
		if !strings.Contains(text, name) {
			t.Fatalf("missing notice: %s", name)
		}
	}
}
