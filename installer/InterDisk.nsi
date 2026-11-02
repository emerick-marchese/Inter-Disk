; Installateur Windows d'Inter Disk (NSIS 3).
; Compilé par build.sh :
;   makensis -DVERSION=x.y.z -DBINDIR=<dossier des .exe> -DOUTFILE=<setup.exe> installer/InterDisk.nsi
; BINDIR doit contenir InterDisk-amd64.exe, InterDisk-arm64.exe et LISEZMOI.txt.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
RequestExecutionLevel admin

!include "MUI2.nsh"
!include "x64.nsh"
!include "LogicLib.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef ASSETS
  !define ASSETS "../assets"   ; relatif au dossier de ce script
!endif
!define APP      "Inter Disk"
!define EXE      "InterDisk.exe"
!define REGKEY   "Software\Inter Disk"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Inter Disk"
!define UNINSTEXE "Desinstaller Inter Disk.exe"
!define OLDKEY    "Software\Microsoft\Windows\CurrentVersion\Uninstall\CableDrop"

Name "${APP}"
Caption "Installation de ${APP} ${VERSION}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\${APP}"
InstallDirRegKey HKLM "${REGKEY}" "InstallDir"
BrandingText "${APP} ${VERSION}"
ShowInstDetails nevershow
ShowUninstDetails nevershow

; ------------------------------------------------------------ pages
!define MUI_ICON "${ASSETS}/interdisk.ico"
!define MUI_UNICON "${ASSETS}/interdisk.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEFINISHPAGE_BITMAP "${ASSETS}/installer-side.bmp"
!define MUI_WELCOMEPAGE_TITLE "Installation de ${APP}"
!define MUI_WELCOMEPAGE_TEXT "${APP} permet d'envoyer des fichiers d'un PC à un autre, par câble Ethernet (RJ45) ou par Wi-Fi, sans compte ni mot de passe : l'autre PC n'a qu'à accepter.$\r$\n$\r$\nL'installation autorise aussi ${APP} dans le pare-feu Windows : vous n'aurez rien à régler.$\r$\n$\r$\nInstallez ${APP} sur les deux PC."
!define MUI_COMPONENTSPAGE_NODESC
!define MUI_FINISHPAGE_TITLE "${APP} est installé"
!define MUI_FINISHPAGE_TEXT "Lancez ${APP} sur les deux PC, depuis le menu Démarrer ou le Bureau."
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Lancer ${APP} maintenant"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApp

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "French"

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=${LANG_FRENCH} "ProductName" "${APP}"
VIAddVersionKey /LANG=${LANG_FRENCH} "FileDescription" "Installation de ${APP}"
VIAddVersionKey /LANG=${LANG_FRENCH} "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_FRENCH} "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_FRENCH} "LegalCopyright" "emerick-marchese"

; ------------------------------------------------------------ installation
Function .onInit
  ${IfNot} ${IsNativeAMD64}
  ${AndIfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "${APP} nécessite Windows 64 bits (Windows 10 ou 11)."
    Abort
  ${EndIf}
  SetRegView 64
FunctionEnd

Function StopApp
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${EXE}'
  Pop $0
  Sleep 400
FunctionEnd

; Inter Disk remplace CableDrop (ancien nom) : on le désinstalle s'il est présent.
Function RemoveCableDrop
  ReadRegStr $0 HKLM "${OLDKEY}" "InstallLocation"
  StrCmp $0 "" done
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM CableDrop.exe'
  Pop $1
  IfFileExists "$0\Desinstaller CableDrop.exe" 0 done
  ExecWait '"$0\Desinstaller CableDrop.exe" /S _?=$0'
  Delete "$0\Desinstaller CableDrop.exe"
  RMDir "$0"
  done:
FunctionEnd

Section "${APP} (obligatoire)" SecMain
  SectionIn RO
  Call StopApp
  Call RemoveCableDrop
  SetOutPath "$INSTDIR"
  ${If} ${IsNativeARM64}
    File "/oname=${EXE}" "${BINDIR}/InterDisk-arm64.exe"
  ${Else}
    File "/oname=${EXE}" "${BINDIR}/InterDisk-amd64.exe"
  ${EndIf}
  File "/oname=InterDisk.ico" "${ASSETS}/interdisk.ico"
  File "${BINDIR}/LISEZMOI.txt"
  WriteUninstaller "$INSTDIR\${UNINSTEXE}"

  ; Pare-feu : autorise Inter Disk sur tous les réseaux, y compris le
  ; « Réseau non identifié » (public) d'un câble direct entre deux PC.
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name=all program="$INSTDIR\${EXE}"'
  Pop $0
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="${APP}" dir=in action=allow program="$INSTDIR\${EXE}" enable=yes profile=any'
  Pop $0

  SetShellVarContext all
  CreateShortCut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\InterDisk.ico" 0 SW_SHOWNORMAL "" "Transférer des fichiers entre deux PC"

  WriteRegStr HKLM "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${APP}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "emerick-marchese"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\InterDisk.ico"
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\${UNINSTEXE}"'
  WriteRegStr HKLM "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\${UNINSTEXE}" /S'
  WriteRegStr HKLM "${UNINSTKEY}" "URLInfoAbout" "https://github.com/emerick-marchese/Inter-Disk"
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" 9000
SectionEnd

Section "Raccourci sur le Bureau" SecDesktop
  SetShellVarContext all
  CreateShortCut "$DESKTOP\${APP}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\InterDisk.ico" 0 SW_SHOWNORMAL "" "Transférer des fichiers entre deux PC"
SectionEnd

; Lance Inter Disk avec les droits de l'utilisateur (pas en administrateur).
Function LaunchApp
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${EXE}"'
FunctionEnd

; ------------------------------------------------------------ désinstallation
Function un.onInit
  SetRegView 64
FunctionEnd

Section "Uninstall"
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${EXE}'
  Pop $0
  Sleep 400
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name=all program="$INSTDIR\${EXE}"'
  Pop $0
  SetShellVarContext all
  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$DESKTOP\${APP}.lnk"
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\InterDisk.ico"
  Delete "$INSTDIR\LISEZMOI.txt"
  Delete "$INSTDIR\${UNINSTEXE}"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "${UNINSTKEY}"
  DeleteRegKey HKLM "${REGKEY}"
SectionEnd
