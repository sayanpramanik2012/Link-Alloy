//go:build windows

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	routeJournalVersion = 4
)

var (
	linkAlloyIPv4Routes = []string{"0.0.0.0/1", "128.0.0.0/1"}
	linkAlloyIPv6Routes = []string{"::/1", "8000::/1"}
)

type routeJournal struct {
	Version       int    `json:"version"`
	Interface     string `json:"interface"`
	AdapterGUID   string `json:"adapterGuid"`
	AdapterIfType uint32 `json:"adapterIfType"`
	TunnelType    string `json:"tunnelType"`
	LUID          uint64 `json:"luid"`
	IPv4Index     uint32 `json:"ipv4Index"`
	IPv6Index     uint32 `json:"ipv6Index"`
	IPv4Address   string `json:"ipv4Address"`
	IPv6Address   string `json:"ipv6Address"`
	NextHopIPv4   string `json:"nextHopIPv4"`
	NextHopIPv6   string `json:"nextHopIPv6"`
}

type windowsRouteManager struct {
	mu          sync.Mutex
	journal     *routeJournal
	journalPath string
}

func NewRouteManager() RouteManager { return &windowsRouteManager{} }

func (m *windowsRouteManager) Recover(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	journal, err := m.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := m.recoverJournal(ctx, journal); err != nil {
		return err
	}
	if err := m.removeJournal(); err != nil {
		return err
	}
	m.journal = nil
	return nil
}

func (m *windowsRouteManager) Install(ctx context.Context, device TunnelDevice) error {
	if device == nil {
		return errors.New("a tunnel device is required before installing routes")
	}
	if err := requireAdministrator(); err != nil {
		return err
	}
	ipv4Index, ipv6Index := device.InterfaceIndexes()
	if ipv4Index == 0 || ipv6Index == 0 || device.InterfaceLUID() == 0 {
		return errors.New("Windows did not provide valid interface identifiers for the tunnel")
	}
	journal := routeJournal{
		Version:       routeJournalVersion,
		Interface:     device.InterfaceName(),
		TunnelType:    linkAlloyTunnelType,
		AdapterIfType: ifTypePropVirtual,
		LUID:          device.InterfaceLUID(),
		IPv4Index:     ipv4Index,
		IPv6Index:     ipv6Index,
		IPv4Address:   tunnelIPv4Address,
		IPv6Address:   tunnelIPv6Address,
		NextHopIPv4:   "0.0.0.0",
		NextHopIPv6:   "::",
	}
	if guid, guidErr := interfaceGUIDFromLUID(journal.LUID); guidErr != nil {
		return fmt.Errorf("identify Link Alloy tunnel adapter: %w", guidErr)
	} else {
		journal.AdapterGUID = formatGUID(guid)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if stale, readErr := m.readJournal(); readErr == nil {
		if cleanupErr := m.recoverJournal(ctx, stale); cleanupErr != nil {
			return fmt.Errorf("recover earlier tunnel routes before starting: %w", cleanupErr)
		}
		if removeErr := m.removeJournal(); removeErr != nil {
			return removeErr
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := m.verifyInterface(&journal); err != nil {
		return fmt.Errorf("verify tunnel adapter: %w", err)
	}
	if err := runPowerShell(ctx, routePreflightScript(journal)); err != nil {
		return fmt.Errorf("route preflight failed without changing network settings: %w", err)
	}
	if err := m.writeJournal(journal); err != nil {
		return fmt.Errorf("save tunnel cleanup record: %w", err)
	}
	if err := runPowerShell(ctx, routeInstallScript(journal)); err != nil {
		return m.rollbackFailedInstall(journal, err)
	}
	if err := runPowerShell(ctx, routeVerifyScript(journal)); err != nil {
		return m.rollbackFailedInstall(journal, fmt.Errorf("post-install route verification: %w", err))
	}
	m.journal = &journal
	return nil
}

func (m *windowsRouteManager) rollbackFailedInstall(journal routeJournal, installErr error) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cleanupErr := removeRoutesAndAddresses(cleanupCtx, journal)
	cancel()
	if cleanupErr == nil {
		cleanupErr = m.removeJournal()
	}
	if cleanupErr != nil {
		m.journal = &journal
		return fmt.Errorf("%v; automatic rollback is incomplete and remains journaled: %w", installErr, cleanupErr)
	}
	m.journal = nil
	return installErr
}

func (m *windowsRouteManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	journal := m.journal
	if journal == nil {
		var err error
		journal, err = m.readJournal()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if err := m.recoverJournal(ctx, journal); err != nil {
		return err
	}
	if err := m.removeJournal(); err != nil {
		return err
	}
	m.journal = nil
	return nil
}

func (m *windowsRouteManager) verifyInterface(journal *routeJournal) error {
	identity, err := inspectWintunAdapter(journal.LUID)
	if err != nil {
		return err
	}
	return validateWintunIdentity(journal, identity)
}

func (m *windowsRouteManager) recoverJournal(ctx context.Context, journal *routeJournal) error {
	if verifyErr := m.verifyInterface(journal); verifyErr != nil {
		residual, residualErr := journalHasResidualSettings(ctx, *journal)
		if residualErr != nil {
			return errors.Join(verifyErr, residualErr)
		}
		if residual {
			return fmt.Errorf("refuse cleanup because the adapter cannot be verified and its recorded routes or addresses remain: %w", verifyErr)
		}
		// Windows may have removed the transient adapter and its routes when a
		// process was killed. With no recorded settings left, only the stale
		// journal needs clearing.
		return nil
	}
	return removeRoutesAndAddresses(ctx, *journal)
}

func (m *windowsRouteManager) readJournal() (*routeJournal, error) {
	path, err := m.path()
	if err != nil {
		return nil, err
	}
	data, err := readProtectedState(path)
	legacy := false
	if errors.Is(err, os.ErrNotExist) {
		legacyPath, legacyErr := legacyJournalPath()
		if legacyErr != nil {
			return nil, legacyErr
		}
		data, err = readLegacyJournal(legacyPath)
		legacy = err == nil
	}
	if err != nil {
		return nil, err
	}
	var journal routeJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("read tunnel cleanup record: %w", err)
	}
	switch journal.Version {
	case 1:
		journal.NextHopIPv4 = "0.0.0.0"
		journal.NextHopIPv6 = "::"
	case 2:
		journal.NextHopIPv4 = stackIPv4Address
		journal.NextHopIPv6 = stackIPv6Address
	case 3:
		if journal.NextHopIPv4 == "" || journal.NextHopIPv6 == "" {
			return nil, errors.New("tunnel cleanup record is missing route next hops")
		}
	case routeJournalVersion:
		if journal.AdapterGUID == "" || journal.AdapterIfType == 0 || journal.TunnelType == "" {
			return nil, errors.New("protected tunnel cleanup record lacks adapter identity")
		}
	default:
		return nil, errors.New("tunnel cleanup record is from an unsupported version")
	}
	if journal.LUID == 0 || journal.IPv4Index == 0 || journal.IPv6Index == 0 || !validJournalAddresses(&journal) {
		return nil, errors.New("tunnel cleanup record is invalid or from an unsupported version")
	}
	if legacy {
		journal.Version = 3
	}
	return &journal, nil
}

func (m *windowsRouteManager) writeJournal(journal routeJournal) error {
	path, err := m.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeProtectedState(path, data)
}

func (m *windowsRouteManager) removeJournal() error {
	path, err := m.path()
	if err != nil {
		return err
	}
	if err := removeProtectedState(path); err != nil {
		return err
	}
	legacyPath, err := legacyJournalPath()
	if err != nil {
		return err
	}
	return removeLegacyJournal(legacyPath)
}

func (m *windowsRouteManager) path() (string, error) {
	if m.journalPath != "" {
		return m.journalPath, nil
	}
	path, err := protectedJournalPath()
	if err != nil {
		return "", err
	}
	m.journalPath = path
	return m.journalPath, nil
}

func validJournalAddresses(j *routeJournal) bool {
	if j.Interface != linkAlloyTunnelName || j.IPv4Address != tunnelIPv4Address || j.IPv6Address != tunnelIPv6Address {
		return false
	}
	if !((j.NextHopIPv4 == "0.0.0.0" && j.NextHopIPv6 == "::") || (j.NextHopIPv4 == stackIPv4Address && j.NextHopIPv6 == stackIPv6Address)) {
		return false
	}
	return netip.MustParseAddr(tunnelIPv4Address).Is4() && netip.MustParseAddr(tunnelIPv6Address).Is6()
}

func routePreflightScript(journal routeJournal) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ipv4Index = [uint32]%d
$ipv6Index = [uint32]%d
$ipv4Address = '%s'
$ipv6Address = '%s'
$prefixes = @('0.0.0.0/1', '128.0.0.0/1', '::/1', '8000::/1', '198.18.0.0/15', 'fd00:6c69:6e6b::/64')
foreach ($prefix in $prefixes) {
  $existing = @(Get-NetRoute -DestinationPrefix $prefix -PolicyStore ActiveStore -ErrorAction SilentlyContinue)
  if ($existing.Count -gt 0) { throw "Route prefix already exists: $prefix" }
}
if (Get-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress $ipv4Address -ErrorAction SilentlyContinue) { throw 'Tunnel IPv4 address is already configured' }
if (Get-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress $ipv6Address -ErrorAction SilentlyContinue) { throw 'Tunnel IPv6 address is already configured' }
`, journal.IPv4Index, journal.IPv6Index, tunnelIPv4Address, tunnelIPv6Address)
}

func routeInstallScript(journal routeJournal) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ipv4Index = [uint32]%d
$ipv6Index = [uint32]%d
New-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress '%s' -PrefixLength %d -SkipAsSource $false -PolicyStore ActiveStore | Out-Null
New-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress '%s' -PrefixLength %d -SkipAsSource $false -PolicyStore ActiveStore | Out-Null
$deadline = (Get-Date).AddSeconds(5)
do {
  $v4 = @(Get-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress '%s' -ErrorAction SilentlyContinue)
  $v6 = @(Get-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress '%s' -ErrorAction SilentlyContinue)
  if ($v4.Count -eq 1 -and $v6.Count -eq 1 -and $v4[0].AddressState -eq 'Preferred' -and $v6[0].AddressState -eq 'Preferred') { break }
  Start-Sleep -Milliseconds 100
} while ((Get-Date) -lt $deadline)
if ($v4.Count -ne 1 -or $v6.Count -ne 1 -or $v4[0].AddressState -ne 'Preferred' -or $v6[0].AddressState -ne 'Preferred') { throw 'Tunnel addresses did not reach Preferred state' }
New-NetRoute -DestinationPrefix '0.0.0.0/1' -InterfaceIndex $ipv4Index -NextHop '%s' -RouteMetric 1 -PolicyStore ActiveStore | Out-Null
New-NetRoute -DestinationPrefix '128.0.0.0/1' -InterfaceIndex $ipv4Index -NextHop '%s' -RouteMetric 1 -PolicyStore ActiveStore | Out-Null
New-NetRoute -DestinationPrefix '::/1' -InterfaceIndex $ipv6Index -NextHop '%s' -RouteMetric 1 -PolicyStore ActiveStore | Out-Null
New-NetRoute -DestinationPrefix '8000::/1' -InterfaceIndex $ipv6Index -NextHop '%s' -RouteMetric 1 -PolicyStore ActiveStore | Out-Null
`, journal.IPv4Index, journal.IPv6Index, journal.IPv4Address, tunnelIPv4Prefix, journal.IPv6Address, tunnelIPv6Prefix, journal.IPv4Address, journal.IPv6Address, journal.NextHopIPv4, journal.NextHopIPv4, journal.NextHopIPv6, journal.NextHopIPv6)
}

func routeVerifyScript(journal routeJournal) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ipv4Index = [uint32]%d
$ipv6Index = [uint32]%d
$ipv4Address = '%s'
$ipv6Address = '%s'
$routes = @(
  @{Prefix='0.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='128.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='::/1'; Index=$ipv6Index; NextHop='%s'},
  @{Prefix='8000::/1'; Index=$ipv6Index; NextHop='%s'}
)
foreach ($expected in $routes) {
  $found = @(Get-NetRoute -DestinationPrefix $expected.Prefix -InterfaceIndex $expected.Index -NextHop $expected.NextHop -PolicyStore ActiveStore -ErrorAction Stop)
  if ($found.Count -ne 1) { throw "Expected one installed route for $($expected.Prefix), found $($found.Count)" }
}
$v4 = @(Get-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress $ipv4Address -ErrorAction Stop)
$v6 = @(Get-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress $ipv6Address -ErrorAction Stop)
if ($v4.Count -ne 1 -or $v6.Count -ne 1) { throw 'Tunnel interface addresses could not be verified' }
$interface4 = Get-NetIPInterface -InterfaceIndex $ipv4Index -AddressFamily IPv4 -ErrorAction Stop
$interface6 = Get-NetIPInterface -InterfaceIndex $ipv6Index -AddressFamily IPv6 -ErrorAction Stop
try {
  $best4 = @(Find-NetRoute -RemoteIPAddress '8.8.8.8' -ErrorAction Stop | Where-Object { $_.PSObject.Properties.Name -contains 'DestinationPrefix' })
} catch {
  $state = $interface4 | Select-Object InterfaceAlias,InterfaceIndex,ConnectionState,InterfaceMetric,Dhcp
  $route = Get-NetRoute -DestinationPrefix '0.0.0.0/1' -InterfaceIndex $ipv4Index -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Select-Object DestinationPrefix,InterfaceIndex,NextHop,RouteMetric
  throw "IPv4 selection failed: $($_.Exception.Message); interface=$($state | ConvertTo-Json -Compress); route=$($route | ConvertTo-Json -Compress)"
}
try {
  $best6 = @(Find-NetRoute -RemoteIPAddress '2606:4700:4700::1111' -ErrorAction Stop | Where-Object { $_.PSObject.Properties.Name -contains 'DestinationPrefix' })
} catch {
  $state = $interface6 | Select-Object InterfaceAlias,InterfaceIndex,ConnectionState,InterfaceMetric,Dhcp
  $route = Get-NetRoute -DestinationPrefix '::/1' -InterfaceIndex $ipv6Index -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Select-Object DestinationPrefix,InterfaceIndex,NextHop,RouteMetric
  throw "IPv6 selection failed: $($_.Exception.Message); interface=$($state | ConvertTo-Json -Compress); route=$($route | ConvertTo-Json -Compress)"
}
if ($best4.Count -lt 1 -or $best4[0].InterfaceIndex -ne $ipv4Index) { throw 'Windows would not select the tunnel for an IPv4 public route' }
if ($best6.Count -lt 1 -or $best6[0].InterfaceIndex -ne $ipv6Index) { throw 'Windows would not select the tunnel for an IPv6 public route' }
`, journal.IPv4Index, journal.IPv6Index, journal.IPv4Address, journal.IPv6Address, journal.NextHopIPv4, journal.NextHopIPv4, journal.NextHopIPv6, journal.NextHopIPv6)
}

func routeRemoveScript(journal routeJournal) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ipv4Index = [uint32]%d
$ipv6Index = [uint32]%d
$ipv4Address = '%s'
$ipv6Address = '%s'
$routeKeys = @(
  @{Prefix='0.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='128.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='::/1'; Index=$ipv6Index; NextHop='%s'},
  @{Prefix='8000::/1'; Index=$ipv6Index; NextHop='%s'}
)
foreach ($key in $routeKeys) {
  $routes = @(Get-NetRoute -DestinationPrefix $key.Prefix -InterfaceIndex $key.Index -NextHop $key.NextHop -PolicyStore ActiveStore -ErrorAction SilentlyContinue)
  foreach ($route in $routes) { Remove-NetRoute -InputObject $route -Confirm:$false -ErrorAction Stop }
}
foreach ($item in @(@{Index=$ipv4Index; Address=$ipv4Address}, @{Index=$ipv6Index; Address=$ipv6Address})) {
  $addresses = @(Get-NetIPAddress -InterfaceIndex $item.Index -IPAddress $item.Address -ErrorAction SilentlyContinue)
  foreach ($address in $addresses) { Remove-NetIPAddress -InputObject $address -Confirm:$false -ErrorAction Stop }
}
$remainingRoutes = @()
foreach ($key in $routeKeys) { $remainingRoutes += @(Get-NetRoute -DestinationPrefix $key.Prefix -InterfaceIndex $key.Index -NextHop $key.NextHop -PolicyStore ActiveStore -ErrorAction SilentlyContinue) }
$remainingAddresses = @(
  Get-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress $ipv4Address -ErrorAction SilentlyContinue
  Get-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress $ipv6Address -ErrorAction SilentlyContinue
)
if ($remainingRoutes.Count -gt 0 -or $remainingAddresses.Count -gt 0) { throw 'Tunnel route cleanup could not be verified' }
`, journal.IPv4Index, journal.IPv6Index, journal.IPv4Address, journal.IPv6Address, journal.NextHopIPv4, journal.NextHopIPv4, journal.NextHopIPv6, journal.NextHopIPv6)
}

func removeRoutesAndAddresses(ctx context.Context, journal routeJournal) error {
	return runPowerShell(ctx, routeRemoveScript(journal))
}

func journalHasResidualSettings(ctx context.Context, journal routeJournal) (bool, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ipv4Index = [uint32]%d
$ipv6Index = [uint32]%d
$ipv4Address = '%s'
$ipv6Address = '%s'
$routeKeys = @(
  @{Prefix='0.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='128.0.0.0/1'; Index=$ipv4Index; NextHop='%s'},
  @{Prefix='::/1'; Index=$ipv6Index; NextHop='%s'},
  @{Prefix='8000::/1'; Index=$ipv6Index; NextHop='%s'}
)
$routes = @()
foreach ($key in $routeKeys) { $routes += @(Get-NetRoute -DestinationPrefix $key.Prefix -InterfaceIndex $key.Index -NextHop $key.NextHop -PolicyStore ActiveStore -ErrorAction SilentlyContinue) }
$addresses = @(
  Get-NetIPAddress -InterfaceIndex $ipv4Index -IPAddress $ipv4Address -ErrorAction SilentlyContinue
  Get-NetIPAddress -InterfaceIndex $ipv6Index -IPAddress $ipv6Address -ErrorAction SilentlyContinue
)
if ($routes.Count -gt 0 -or $addresses.Count -gt 0) { 'true' } else { 'false' }
`, journal.IPv4Index, journal.IPv6Index, journal.IPv4Address, journal.IPv6Address, journal.NextHopIPv4, journal.NextHopIPv4, journal.NextHopIPv6, journal.NextHopIPv6)
	output, err := runPowerShellOutput(ctx, script)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(output), "true"), nil
}

func runPowerShell(ctx context.Context, script string) error {
	_, err := runPowerShellOutput(ctx, script)
	return err
}

func runPowerShellOutput(ctx context.Context, script string) (string, error) {
	command := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
