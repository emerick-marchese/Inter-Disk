#!/usr/bin/env bash
# Compile CableDrop pour Windows, macOS et Linux dans dist/
set -euo pipefail
cd "$(dirname "$0")"
VERSION=$(sed -n 's/.*appVersion = "\(.*\)".*/\1/p' main.go)
rm -rf dist && mkdir -p dist/tmp

build() { # GOOS GOARCH sortie
  echo "→ $1/$2"
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w" -o "$3" .
}

# Notice texte pour Windows (fins de ligne CRLF)
sed 's/$/\r/' README.md > dist/tmp/LISEZMOI.txt

for arch in amd64 arm64; do
  name=$([ "$arch" = amd64 ] && echo x64 || echo ARM64)
  d=dist/tmp/win-$arch && mkdir -p "$d"
  build windows $arch "$d/CableDrop.exe"
  cp autoriser-pare-feu.bat dist/tmp/LISEZMOI.txt "$d/"
  (cd "$d" && zip -q -9 "../../CableDrop-$VERSION-Windows-$name.zip" CableDrop.exe autoriser-pare-feu.bat LISEZMOI.txt)
done

for arch in arm64 amd64; do
  name=$([ "$arch" = arm64 ] && echo AppleSilicon || echo Intel)
  d=dist/tmp/mac-$arch && mkdir -p "$d"
  build darwin $arch "$d/CableDrop-macos"
  cp README.md "$d/LISEZMOI.md"
  tar -C "$d" -czf "dist/CableDrop-$VERSION-macOS-$name.tar.gz" CableDrop-macos LISEZMOI.md
done

d=dist/tmp/linux && mkdir -p "$d"
build linux amd64 "$d/cabledrop"
cp README.md "$d/LISEZMOI.md"
tar -C "$d" -czf "dist/CableDrop-$VERSION-Linux-x64.tar.gz" cabledrop LISEZMOI.md

rm -rf dist/tmp
ls -lh dist
