#!/usr/bin/env bash
# Compile CableDrop pour Windows, macOS et Linux dans dist/,
# puis l'installateur Windows CableDrop-Setup.exe (si NSIS est installé :
# « sudo apt install nsis » sous Linux, ou https://nsis.sourceforge.io).
set -euo pipefail
cd "$(dirname "$0")"
VERSION=$(sed -n 's/.*appVersion = "\(.*\)".*/\1/p' main.go)
echo "CableDrop $VERSION"
rm -rf dist && mkdir -p dist/tmp/win

build() { # GOOS GOARCH sortie
  echo "→ $1/$2"
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w" -o "$3" .
}

# Notice texte pour Windows (fins de ligne CRLF)
sed 's/$/\r/' README.md > dist/tmp/win/LISEZMOI.txt

# Windows : version portable (zip) pour chaque architecture
for arch in amd64 arm64; do
  name=$([ "$arch" = amd64 ] && echo x64 || echo ARM64)
  build windows $arch "dist/tmp/win/CableDrop-$arch.exe"
  d=dist/tmp/zip-$arch && mkdir -p "$d"
  cp "dist/tmp/win/CableDrop-$arch.exe" "$d/CableDrop.exe"
  cp autoriser-pare-feu.bat dist/tmp/win/LISEZMOI.txt "$d/"
  (cd "$d" && zip -q -9 "../../CableDrop-Windows-$name-portable.zip" CableDrop.exe autoriser-pare-feu.bat LISEZMOI.txt)
done

# Windows : installateur (un seul setup.exe pour x64 et ARM64)
if command -v makensis >/dev/null; then
  echo "→ installateur Windows"
  makensis -V2 -DVERSION="$VERSION" -DASSETS="$PWD/assets" -DBINDIR="$PWD/dist/tmp/win" -DOUTFILE="$PWD/dist/CableDrop-Setup.exe" installer/CableDrop.nsi
  cp dist/CableDrop-Setup.exe CableDrop-Setup.exe   # copie publiée dans le dépôt GitHub
else
  echo "⚠ makensis introuvable : installateur non généré (sudo apt install nsis)"
fi

# macOS
for arch in arm64 amd64; do
  name=$([ "$arch" = arm64 ] && echo AppleSilicon || echo Intel)
  d=dist/tmp/mac-$arch && mkdir -p "$d"
  build darwin $arch "$d/CableDrop-macos"
  cp README.md "$d/LISEZMOI.md"
  tar -C "$d" -czf "dist/CableDrop-macOS-$name.tar.gz" CableDrop-macos LISEZMOI.md
done

# Linux
d=dist/tmp/linux && mkdir -p "$d"
build linux amd64 "$d/cabledrop"
cp README.md "$d/LISEZMOI.md"
tar -C "$d" -czf "dist/CableDrop-Linux-x64.tar.gz" cabledrop LISEZMOI.md

rm -rf dist/tmp
ls -lh dist
