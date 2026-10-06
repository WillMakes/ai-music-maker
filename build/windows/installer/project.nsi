Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
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
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"
!include "FileFunc.nsh"
!include "WordFunc.nsh"

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
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipDirectoryOnUpdate
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

#### Updating an existing install (#131)
##
## An installer run over an installed copy is an update, done in place: the
## same folder, so the Start menu and desktop shortcuts still point at the
## program, and so do the MCP entries assistants keep for it (Settings -> AI
## assistants writes this exe's full path into their configs). The files are
## overwritten rather than the old copy uninstalled first, because the
## uninstaller also clears the WebView2 data folder, and an update should not
## cost the producer anything an uninstall would.
Var UpdatingFrom # The installed version this run replaces; empty on a fresh install.

Function .onInit
   !insertmacro wails.checkArchitecture

   SetRegView 64
   ReadRegStr $0 HKLM "${UNINST_KEY}" "UninstallString"
   ${If} $0 == ""
       ReadRegStr $0 HKCU "${UNINST_KEY}" "UninstallString"
       ReadRegStr $1 HKCU "${UNINST_KEY}" "DisplayVersion"
   ${Else}
       ReadRegStr $1 HKLM "${UNINST_KEY}" "DisplayVersion"
   ${EndIf}
   ${If} $0 == ""
       Return
   ${EndIf}

   # A newer copy is not silently replaced by an older one. Equal is allowed:
   # running the same installer again is how a broken install gets repaired.
   ${VersionCompare} "$1" "${INFO_PRODUCTVERSION}" $2
   ${If} $2 == 1
       IfSilent downgradeRefused
       MessageBox MB_YESNO|MB_ICONQUESTION "${INFO_PRODUCTNAME} $1 is installed, which is newer than this installer's ${INFO_PRODUCTVERSION}.$\r$\n$\r$\nReplace it with the older version?" IDYES downgradeAccepted
       downgradeRefused:
           Abort
       downgradeAccepted:
   ${EndIf}

   # UninstallString is the quoted path of uninstall.exe inside the install.
   StrCpy $3 $0 "" 1
   StrCpy $3 $3 -1
   ${GetParent} $3 $INSTDIR
   StrCpy $UpdatingFrom $1
FunctionEnd

# An update goes where the installed copy is; asking for a folder would only
# offer a way to end up with two.
Function SkipDirectoryOnUpdate
   ${If} $UpdatingFrom != ""
       Abort
   ${EndIf}
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    # A running copy holds its exe open and the overwrite would fail. Closed
    # here, after the producer has agreed to install, rather than in .onInit.
    # The app saves as it goes, so nothing unsaved is lost to it.
    FindWindow $0 "" "${INFO_PRODUCTNAME}"
    ${If} $0 != 0
        IfSilent closeRunning
        MessageBox MB_OKCANCEL|MB_ICONINFORMATION "${INFO_PRODUCTNAME} is open and will be closed to install the update." IDOK closeRunning
        Abort
    ${EndIf}
    closeRunning:
    nsExec::ExecToLog 'taskkill /f /im "${PRODUCT_EXECUTABLE}"'
    Sleep 1000

    SetOutPath $INSTDIR

    !insertmacro wails.files

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
