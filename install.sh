#!/usr/bin/env sh
# vitals installer: downloads the latest release binary for this OS/arch.
#   curl -fsSL https://raw.githubusercontent.com/gautampachnanda101/vitals/main/install.sh | sh
# Env: VITALS_VERSION (default: latest), VITALS_BIN_DIR (default: /usr/local/bin or ~/.local/bin)
set -eu

repo="gautampachnanda101/vitals"
os="$(uname -s)"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="x86_64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  Darwin|Linux) ;;
  *) echo "unsupported OS: $os — on Windows use: scoop install vitals" >&2; exit 1 ;;
esac

version="${VITALS_VERSION:-latest}"
if [ "$version" = "latest" ]; then
  base="https://github.com/${repo}/releases/latest/download"
else
  base="https://github.com/${repo}/releases/download/${version}"
fi
asset="vitals_${os}_${arch}.tar.gz"
url="${base}/${asset}"

bindir="${VITALS_BIN_DIR:-}"
if [ -z "$bindir" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then bindir=/usr/local/bin; else bindir="$HOME/.local/bin"; fi
fi
mkdir -p "$bindir"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/$asset"
tar -C "$tmp" -xzf "$tmp/$asset"
install -m 0755 "$tmp/vitals" "$bindir/vitals"

echo "Installed $("$bindir/vitals" version) to $bindir/vitals"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "Add $bindir to your PATH." ;;
esac
echo "Try: vitals doctor"
