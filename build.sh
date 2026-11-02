#!/usr/bin/env bash
# Compile Inter Disk dans dist/ :
#   - InterDisk-Setup.exe : installateur Windows (x64 + ARM64), si NSIS est installé
#   - versions portables Windows (zip)
#   - version Linux (si les bibliothèques GTK/WebKit de développement sont présentes)
# Prérequis : Go (version indiquée dans go.mod), NSIS (« sudo apt install nsis »).
set -euo pipefail
cd "$(dirname "$0")"
VERSION=$(sed -n 's/.*appVersion = "\(.*\)".*/\1/p' main.go)
echo "Inter Disk $VERSION"
rm -rf dist && mkdir -p dist/tmp/win

# Notice texte pour Windows (fins de ligne CRLF)
sed 's/$/\r/' README.md > dist/tmp/win/LISEZMOI.txt

# Windows : application fenêtrée (pas de console), sans CGO
for arch in amd64 arm64; do
  echo "→ windows/$arch"
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -tags desktop,production -trimpath \
    -ldflags "-s -w -H windowsgui" -o "dist/tmp/win/InterDisk-$arch.exe" .
  name=$([ "$arch" = amd64 ] && echo x64 || echo ARM64)
  d=dist/tmp/zip-$arch && mkdir -p "$d"
  cp "dist/tmp/win/InterDisk-$arch.exe" "$d/InterDisk.exe"
  cp autoriser-pare-feu.bat dist/tmp/win/LISEZMOI.txt "$d/"
  (cd "$d" && zip -q -9 "../../InterDisk-Windows-$name-portable.zip" InterDisk.exe autoriser-pare-feu.bat LISEZMOI.txt)
done

# Windows : installateur (un seul setup.exe pour x64 et ARM64)
if command -v makensis >/dev/null; then
  echo "→ installateur Windows"
  makensis -V2 -DVERSION="$VERSION" -DASSETS="$PWD/assets" -DBINDIR="$PWD/dist/tmp/win" \
    -DOUTFILE="$PWD/dist/InterDisk-Setup.exe" installer/InterDisk.nsi
else
  echo "⚠ makensis introuvable : installateur non généré (sudo apt install nsis)"
fi

# Linux (nécessite libgtk-3-dev et libwebkit2gtk-4.1-dev pour compiler)
if [ "$(uname -s)" = Linux ] && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
  echo "→ linux/amd64"
  d=dist/tmp/linux && mkdir -p "$d"
  CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -trimpath -ldflags "-s -w" -o "$d/inter-disk" . 2>/dev/null
  cp README.md "$d/LISEZMOI.md"
  tar -C "$d" -czf "dist/InterDisk-Linux-x64.tar.gz" inter-disk LISEZMOI.md
fi

rm -rf dist/tmp
ls -lh dist
