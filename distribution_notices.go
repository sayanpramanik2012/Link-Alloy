package main

import (
	"embed"
	"strings"
)

//go:embed LICENSE THIRD_PARTY_NOTICES.md third-party/licenses/*.txt
var distributionNotices embed.FS

// GetDistributionNotices makes all bundled notices accessible even when the
// portable executable is distributed without an accompanying archive.
func (*NetworkService) GetDistributionNotices() string {
	paths := []string{"LICENSE", "THIRD_PARTY_NOTICES.md"}
	entries, _ := distributionNotices.ReadDir("third-party/licenses")
	for _, entry := range entries {
		paths = append(paths, "third-party/licenses/"+entry.Name())
	}
	var out strings.Builder
	for _, path := range paths {
		data, err := distributionNotices.ReadFile(path)
		if err != nil {
			continue
		}
		out.WriteString("\n\n--- " + path + " ---\n\n")
		out.Write(data)
	}
	return out.String()
}
