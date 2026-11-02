//go:build windows

package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	protectedStateDirectoryName = "Link Alloy"
	protectedStateDirectorySDDL = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	protectedStateFileSDDL      = "O:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)"
	fileAllAccessMask           = 0x001f01ff
	ifTypePropVirtual           = 53
)

type adapterIdentity struct {
	LUID          uint64
	IPv4Index     uint32
	IPv6Index     uint32
	IfType        uint32
	NetworkGUID   windows.GUID
	InterfaceGUID windows.GUID
	FriendlyName  string
	Description   string
}

// ProtectedStateDirectory returns the system-wide state directory path. It
// does not create the directory; writes must call EnsureProtectedStateDirectory.
func ProtectedStateDirectory() (string, error) {
	programData, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("locate the ProgramData directory: %w", err)
	}
	path := filepath.Join(programData, protectedStateDirectoryName)
	if err := ValidateProtectedPath(path); err != nil {
		return "", err
	}
	return path, nil
}

// EnsureProtectedStateDirectory creates or hardens the ProgramData state root
// so only SYSTEM and Administrators can access it.
func EnsureProtectedStateDirectory() error {
	if err := requireAdministrator(); err != nil {
		return err
	}
	path, err := ProtectedStateDirectory()
	if err != nil {
		return err
	}
	return ensureProtectedDirectory(path)
}

// EnsureProtectedSubdirectory creates a child such as "logs" with the same
// protected ACL as the state root.
func EnsureProtectedSubdirectory(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
		return "", errors.New("invalid protected state subdirectory name")
	}
	if err := EnsureProtectedStateDirectory(); err != nil {
		return "", err
	}
	root, err := ProtectedStateDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, name)
	if err := ensureProtectedDirectory(path); err != nil {
		return "", err
	}
	return path, nil
}

// SecureProtectedFile applies the protected file ACL to an existing state or
// log file. Missing files are fine: a later create inherits the protected
// parent ACL and may call this again to make its ACL explicit.
func SecureProtectedFile(path string) error {
	if err := requireAdministrator(); err != nil {
		return err
	}
	if err := ValidateProtectedPath(path); err != nil {
		return err
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("refuse a reparse-point state file: %s", path)
	}
	if attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return fmt.Errorf("expected a file, found a directory: %s", path)
	}
	return applyProtectedACL(path, false)
}

// ValidateProtectedPath rejects paths outside ProgramData\Link Alloy and any
// existing reparse point in the path chain.
func ValidateProtectedPath(path string) error {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return fmt.Errorf("locate the ProgramData directory: %w", err)
	}
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("protected state path is outside ProgramData\\Link Alloy")
	}
	components := strings.Split(relative, string(filepath.Separator))
	if len(components) == 0 || !strings.EqualFold(components[0], protectedStateDirectoryName) {
		return errors.New("protected state path is outside ProgramData\\Link Alloy")
	}
	rootAttrs, err := getAttributes(root)
	if err != nil {
		return fmt.Errorf("inspect ProgramData: %w", err)
	}
	if rootAttrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || rootAttrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("ProgramData is not a regular directory")
	}
	current := root
	for _, component := range components {
		current = filepath.Join(current, component)
		attrs, attrErr := getAttributes(current)
		if errors.Is(attrErr, windows.ERROR_FILE_NOT_FOUND) || errors.Is(attrErr, windows.ERROR_PATH_NOT_FOUND) {
			return nil
		}
		if attrErr != nil {
			return fmt.Errorf("inspect protected path %s: %w", current, attrErr)
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("refuse reparse point in protected state path: %s", current)
		}
	}
	return nil
}

func protectedJournalPath() (string, error) {
	root, err := ProtectedStateDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tunnel-cleanup.json"), nil
}

func legacyJournalPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, protectedStateDirectoryName, "tunnel-cleanup.json"), nil
}

func readProtectedState(path string) ([]byte, error) {
	if err := ValidateProtectedPath(path); err != nil {
		return nil, err
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, errors.New("refuse non-regular protected journal path")
	}
	if err := EnsureProtectedStateDirectory(); err != nil {
		return nil, err
	}
	if err := SecureProtectedFile(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeProtectedState(path string, data []byte) error {
	if err := requireAdministrator(); err != nil {
		return err
	}
	root, err := ProtectedStateDirectory()
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(root) {
		return errors.New("protected journal must be stored directly under the Link Alloy ProgramData directory")
	}
	if err := EnsureProtectedStateDirectory(); err != nil {
		return err
	}
	if err := ValidateProtectedPath(path); err != nil {
		return err
	}
	temporaryPath := path + ".tmp"
	if err := ValidateProtectedPath(temporaryPath); err != nil {
		return err
	}
	if err := os.WriteFile(temporaryPath, data, 0o600); err != nil {
		return err
	}
	if err := SecureProtectedFile(temporaryPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	from, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("replace protected tunnel journal: %w", err)
	}
	return SecureProtectedFile(path)
}

func removeProtectedState(path string) error {
	if err := ValidateProtectedPath(path); err != nil {
		return err
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("refuse to remove a reparse point or directory as the protected journal")
	}
	if err := SecureProtectedFile(path); err != nil {
		return err
	}
	return os.Remove(path)
}

func readLegacyJournal(path string) ([]byte, error) {
	expected, err := legacyJournalPath()
	if err != nil {
		return nil, err
	}
	if filepath.Clean(path) != filepath.Clean(expected) {
		return nil, errors.New("legacy journal path does not match the expected user AppData path")
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, errors.New("refuse reparse point or directory as legacy journal")
	}
	return os.ReadFile(path)
}

func removeLegacyJournal(path string) error {
	expected, err := legacyJournalPath()
	if err != nil {
		return err
	}
	if filepath.Clean(path) != filepath.Clean(expected) {
		return errors.New("legacy journal path does not match the expected user AppData path")
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("refuse to remove reparse point or directory as legacy journal")
	}
	return os.Remove(path)
}

func ensureProtectedDirectory(path string) error {
	if err := ValidateProtectedPath(path); err != nil {
		return err
	}
	attrs, err := getAttributes(path)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		securityDescriptor, err := windows.SecurityDescriptorFromString(protectedStateDirectorySDDL)
		if err != nil {
			return err
		}
		pathPtr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		security := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: securityDescriptor}
		if err := windows.CreateDirectory(pathPtr, security); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return fmt.Errorf("create protected state directory: %w", err)
		}
		attrs, err = getAttributes(path)
	}
	if err != nil {
		return err
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("protected state path is not a regular directory")
	}
	return applyProtectedACL(path, true)
}

func applyProtectedACL(path string, directory bool) error {
	sddl := protectedStateFileSDDL
	if directory {
		sddl = protectedStateDirectorySDDL
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := securityDescriptor.DACL()
	if err != nil {
		return err
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	securityInfo := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, securityInfo, adminSID, nil, dacl, nil); err != nil {
		return fmt.Errorf("set protected state ACL: %w", err)
	}
	return verifyProtectedACL(path, directory)
}

func verifyProtectedACL(path string, directory bool) error {
	securityDescriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := securityDescriptor.Owner()
	if err != nil {
		return err
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(adminSID) {
		return errors.New("protected state owner is not BUILTIN\\Administrators")
	}
	control, _, err := securityDescriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("protected state DACL permits inheritance")
	}
	dacl, _, err := securityDescriptor.DACL()
	if err != nil {
		return err
	}
	if dacl.AceCount != 2 {
		return fmt.Errorf("protected state DACL has %d entries, want only SYSTEM and Administrators", dacl.AceCount)
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	seenSystem, seenAdmins := false, false
	wantFlags := uint8(0)
	if directory {
		wantFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != wantFlags || uint32(ace.Mask) != fileAllAccessMask {
			return errors.New("protected state DACL contains an unexpected access entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(systemSID):
			seenSystem = true
		case sid.Equals(adminSID):
			seenAdmins = true
		default:
			return errors.New("protected state DACL contains an unknown principal")
		}
	}
	if !seenSystem || !seenAdmins {
		return errors.New("protected state DACL must grant access only to SYSTEM and Administrators")
	}
	return nil
}

func getAttributes(path string) (uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(pathPtr)
}

func inspectWintunAdapter(luid uint64) (adapterIdentity, error) {
	var size uint32
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, nil, &size); err != nil && !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
		return adapterIdentity{}, fmt.Errorf("query Windows adapters: %w", err)
	}
	if size == 0 {
		return adapterIdentity{}, errors.New("Windows returned an empty adapter list")
	}
	buffer := make([]byte, size)
	addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, addresses, &size); err != nil {
		return adapterIdentity{}, fmt.Errorf("enumerate Windows adapters: %w", err)
	}
	for current := addresses; current != nil; current = current.Next {
		if current.Luid != luid {
			continue
		}
		guid, err := interfaceGUIDFromLUID(luid)
		if err != nil {
			return adapterIdentity{}, err
		}
		return adapterIdentity{
			LUID: luid, IPv4Index: current.IfIndex, IPv6Index: current.Ipv6IfIndex,
			IfType: current.IfType, NetworkGUID: current.NetworkGuid, InterfaceGUID: guid,
			FriendlyName: windows.UTF16PtrToString(current.FriendlyName),
			Description:  windows.UTF16PtrToString(current.Description),
		}, nil
	}
	return adapterIdentity{}, errors.New("Wintun LUID is not present in the Windows adapter table")
}

func interfaceGUIDFromLUID(luid uint64) (windows.GUID, error) {
	proc := windows.NewLazySystemDLL("iphlpapi.dll").NewProc("ConvertInterfaceLuidToGuid")
	if err := proc.Find(); err != nil {
		return windows.GUID{}, err
	}
	var guid windows.GUID
	status, _, _ := proc.Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&guid)))
	if status != 0 {
		return windows.GUID{}, fmt.Errorf("ConvertInterfaceLuidToGuid returned status 0x%08x", uint32(status))
	}
	return guid, nil
}

func formatGUID(guid windows.GUID) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		guid.Data1, guid.Data2, guid.Data3,
		guid.Data4[0], guid.Data4[1], guid.Data4[2], guid.Data4[3], guid.Data4[4], guid.Data4[5], guid.Data4[6], guid.Data4[7])
}

func validateWintunIdentity(journal *routeJournal, identity adapterIdentity) error {
	if identity.LUID != journal.LUID || identity.IPv4Index != journal.IPv4Index || identity.IPv6Index != journal.IPv6Index {
		return errors.New("tunnel LUID and interface indexes do not match the adapter table")
	}
	if identity.IfType != ifTypePropVirtual {
		return fmt.Errorf("interface type %d is not Wintun's virtual adapter type %d", identity.IfType, ifTypePropVirtual)
	}
	// NetworkGUID describes Windows' network-category profile, not the adapter
	// GUID supplied to Wintun. The protected journal's adapter GUID below is the
	// stable adapter-identity check.
	if !strings.EqualFold(identity.FriendlyName, linkAlloyTunnelName) || !strings.EqualFold(identity.Description, linkAlloyTunnelType+" Tunnel") {
		return errors.New("adapter friendly name or description does not match the Link Alloy Wintun type")
	}
	if journal.AdapterGUID != "" && !strings.EqualFold(journal.AdapterGUID, formatGUID(identity.InterfaceGUID)) {
		return errors.New("adapter interface GUID does not match the protected journal")
	}
	if journal.AdapterIfType != 0 && journal.AdapterIfType != identity.IfType {
		return errors.New("adapter interface type does not match the protected journal")
	}
	if journal.TunnelType != "" && journal.TunnelType != linkAlloyTunnelType {
		return errors.New("tunnel type does not match the Link Alloy Wintun type")
	}
	return nil
}
