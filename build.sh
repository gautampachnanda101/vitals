#!/usr/bin/env bash
# Cross-compile the `vitals` helper binary for every supported platform.
set -euo pipefail

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.version=${VERSION}"

mkdir -p dist

platforms=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

for platform in "${platforms[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"

  out="dist/vitals-${GOOS}-${GOARCH}"
  [ "$GOOS" = "windows" ] && out+=".exe"

  echo "building $GOOS/$GOARCH -> $out"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$out" .
done

echo "[+] version ${VERSION} — binaries in ./dist/"
