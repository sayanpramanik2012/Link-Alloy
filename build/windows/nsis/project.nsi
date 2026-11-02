Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows you to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
## 
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the wails_tools.nsh file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "my-project" # Default "Link_Alloy_Template"
## !define INFO_COMPANYNAME    "My Company" # Default "My Company"
## !define INFO_PRODUCTNAME    "My Product Name" # Default "My Product"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "0.1.0"
## !define INFO_COPYRIGHT      "(c) Now, My Company" # Default "© 2026, My Company"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
## !define WAILS_INSTALL_SCOPE     "user"             # Default "machine" - set to "user" for per-user install ($LOCALAPPDATA) without UAC prompt
####
## Include the wails tools
####
!include "wails_tools.nsh"
!include "LogicLib.nsh"

Var EngineGuard

# Hold the same machine-wide mutex as the GUI/smoke tool throughout installation
# or uninstall. A one-time process check alone leaves a launch/delete race.
!macro EngineGuardFunctions Prefix
Function ${Prefix}AcquireEngineGuard
    System::Call 'user32::FindWindowW(p 0, w "Link Alloy") p.r0'
    ${If} $0 != 0
        MessageBox MB_ICONSTOP "Stop the Link Alloy tunnel and close the app before continuing."
        Abort
    ${EndIf}
    System::Call 'kernel32::CreateMutexW(p 0, i 0, w "Global\LinkAlloy-4F6A9D10-2B57-4F31-A82D-6C696E6B616C") p.r0 ?e'
    Pop $1
    ${If} $0 == 0
        MessageBox MB_ICONSTOP "Could not reserve Link Alloy maintenance access. Close the app and retry."
        Abort
    ${EndIf}
    ${If} $1 == 183
        System::Call 'kernel32::CloseHandle(p r0)'
        MessageBox MB_ICONSTOP "Link Alloy is running. Stop its tunnel and close all app and engine-test windows first."
        Abort
    ${EndIf}
    StrCpy $EngineGuard $0
FunctionEnd
!macroend

!insertmacro EngineGuardFunctions ""
!insertmacro EngineGuardFunctions "un."

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'
!ifdef LINKALLOY_SIGN_RELEASE
    !uninstfinalize 'powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "..\..\..\tools\sign-windows.ps1" -InputPath "%1"'
!endif

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
!else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
   Call AcquireEngineGuard
FunctionEnd

Function un.onInit
   Call un.AcquireEngineGuard
FunctionEnd

Function .onGUIEnd
    ${If} $EngineGuard != 0
        System::Call 'kernel32::CloseHandle(p $EngineGuard)'
    ${EndIf}
FunctionEnd

Function un.onGUIEnd
    ${If} $EngineGuard != 0
        System::Call 'kernel32::CloseHandle(p $EngineGuard)'
    ${EndIf}
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR
    
    !insertmacro wails.files
    File /oname=LICENSE.txt "..\..\..\LICENSE"
    File /oname=THIRD_PARTY_NOTICES.md "..\..\..\THIRD_PARTY_NOTICES.md"
    File "..\..\..\third-party\licenses\LICENSE-Wails.txt"
    File "..\..\..\third-party\licenses\LICENSE-React.txt"
    File "..\..\..\third-party\licenses\LICENSE-ReactDOM.txt"
    File "..\..\..\third-party\licenses\LICENSE-gVisor.txt"
    File "..\..\..\third-party\licenses\LICENSE-Wintun-Go-Wrapper.txt"
    File "..\..\..\third-party\licenses\LICENSE-Wintun-Prebuilt.txt"

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols
    
    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall" 
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
