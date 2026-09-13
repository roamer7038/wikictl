#!/bin/sh
# Install wikictl from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/roamer7038/wikictl/main/install.sh | sh
#
# Environment:
#   WIKICTL_VERSION      release tag to install, such as v0.1.0 (default: latest)
#   WIKICTL_INSTALL_DIR  directory to install into (default: ~/.local/bin)
#
# Supported: Linux and macOS, x86_64 and arm64. Requires curl and sha256sum or shasum.
set -eu

repo=roamer7038/wikictl
version=${WIKICTL_VERSION:-latest}
dir=${WIKICTL_INSTALL_DIR:-$HOME/.local/bin}

fail() { echo "install.sh: $*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux | darwin) ;;
  *) fail "unsupported operating system: $os (Linux and macOS only)" ;;
esac
case "$arch" in
  x86_64 | aarch64 | arm64) ;;
  *) fail "unsupported architecture: $arch (x86_64 and arm64 only)" ;;
esac
command -v curl >/dev/null || fail "curl is required"

# Release assets are named after "uname -s" and "uname -m".
asset="wikictl_${os}_${arch}"
if [ "$version" = latest ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading $base/$asset"
curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/wikictl" "$base/$asset" || fail "download failed: $base/$asset"
curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "download failed: $base/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || fail "no checksum for $asset in checksums.txt"
if command -v sha256sum >/dev/null; then
  actual=$(sha256sum "$tmp/wikictl" | cut -d' ' -f1)
elif command -v shasum >/dev/null; then
  actual=$(shasum -a 256 "$tmp/wikictl" | cut -d' ' -f1)
else
  fail "sha256sum or shasum is required"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

chmod +x "$tmp/wikictl"
mkdir -p "$dir"
mv "$tmp/wikictl" "$dir/wikictl"
echo "installed $("$dir/wikictl" version) to $dir/wikictl"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "note: $dir is not on your PATH; add it, for example: export PATH=\"$dir:\$PATH\"" >&2 ;;
esac
