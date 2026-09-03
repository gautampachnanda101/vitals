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

# Linux desktop entry so `vitals dashboard` is reachable from an app
# launcher/menu without a terminal (roadmap item 004) — best effort,
# never fails the install: no icon/menu on a headless box or an
# unwritable ~/.local/share is a shrug, not an error.
if [ "$os" = "Linux" ]; then
  apps_dir="$HOME/.local/share/applications"
  if mkdir -p "$apps_dir" 2>/dev/null; then
    desktop_url="https://raw.githubusercontent.com/${repo}/main/packaging/linux/vitals.desktop"
    if curl -fsSL "$desktop_url" 2>/dev/null | sed "s|__BIN_PATH__|$bindir/vitals|" > "$apps_dir/vitals.desktop.tmp" 2>/dev/null; then
      mv "$apps_dir/vitals.desktop.tmp" "$apps_dir/vitals.desktop"
      echo "Added a Vitals launcher to your applications menu."
    else
      rm -f "$apps_dir/vitals.desktop.tmp"
    fi
  fi
fi

echo "Try: vitals doctor"
