@echo off
rem ------------------------------------------------------------------
rem  Autorise CableDrop dans le pare-feu Windows, pour TOUS les types
rem  de reseaux (un cable direct entre deux PC est vu comme
rem  "Reseau non identifie" = reseau public).
rem  Double-cliquez sur ce fichier : Windows demandera les droits admin.
rem ------------------------------------------------------------------
net session >nul 2>&1
if %errorlevel% neq 0 (
  echo Demande des droits administrateur...
  powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
  exit /b
)
set "EXE=%~dp0CableDrop.exe"
if not exist "%EXE%" (
  echo CableDrop.exe est introuvable a cote de ce fichier :
  echo   %EXE%
  echo Placez autoriser-pare-feu.bat dans le meme dossier que CableDrop.exe.
  pause
  exit /b 1
)
rem Supprime d'abord les regles existantes (y compris les regles de BLOCAGE
rem creees si "Reseaux publics" n'avait pas ete coche au premier lancement).
netsh advfirewall firewall delete rule name=all program="%EXE%" >nul 2>&1
netsh advfirewall firewall add rule name="CableDrop" dir=in action=allow program="%EXE%" enable=yes profile=any >nul
if %errorlevel% neq 0 (
  echo Echec de la creation de la regle de pare-feu.
  pause
  exit /b 1
)
echo.
echo OK : CableDrop est autorise dans le pare-feu Windows (tous reseaux).
echo Vous pouvez fermer cette fenetre et relancer CableDrop.
echo.
pause
