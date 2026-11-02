//go:build windows

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedRouteScriptsParse(t *testing.T) {
	journal := routeJournal{
		Version: routeJournalVersion, Interface: linkAlloyTunnelName, LUID: 17,
		IPv4Index: 21, IPv6Index: 22, IPv4Address: tunnelIPv4Address, IPv6Address: tunnelIPv6Address,
		NextHopIPv4: "0.0.0.0", NextHopIPv6: "::",
	}
	scripts := map[string]string{
		"preflight": routePreflightScript(journal),
		"install":   routeInstallScript(journal),
		"verify":    routeVerifyScript(journal),
		"remove":    routeRemoveScript(journal),
	}
	parser := "$tokens=$null; $parseErrors=$null; [System.Management.Automation.Language.Parser]::ParseFile($args[0],[ref]$tokens,[ref]$parseErrors) | Out-Null; if ($parseErrors.Count -gt 0) { $parseErrors | ForEach-Object { Write-Error $_ }; exit 1 }"
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name+".ps1")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			parserPath := filepath.Join(dir, "parse.ps1")
			if err := os.WriteFile(parserPath, []byte(parser), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-File", parserPath, path)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("PowerShell parser failed: %v\n%s", err, output)
			}
		})
	}
}

func TestJournalValidationRejectsPowerShellSyntaxAndUnexpectedAdapter(t *testing.T) {
	valid := routeJournal{Version: routeJournalVersion, Interface: linkAlloyTunnelName, LUID: 7, IPv4Index: 8, IPv6Index: 9, IPv4Address: tunnelIPv4Address, IPv6Address: tunnelIPv6Address, NextHopIPv4: "0.0.0.0", NextHopIPv6: "::"}
	if !validJournalAddresses(&valid) { t.Fatal("fixed Link Alloy journal was rejected") }
	cases := []routeJournal{
		func() routeJournal { j:=valid; j.NextHopIPv4="0.0.0.0'; Start-Process calc; '"; return j }(),
		func() routeJournal { j:=valid; j.IPv4Address="198.18.0.2'; Remove-Item C:\\; '"; return j }(),
		func() routeJournal { j:=valid; j.Interface="Ethernet"; return j }(),
		func() routeJournal { j:=valid; j.NextHopIPv6="fd00::1"; return j }(),
	}
	for _, journal := range cases { if validJournalAddresses(&journal) { t.Fatalf("unsafe journal was accepted: %#v", journal) } }
}
