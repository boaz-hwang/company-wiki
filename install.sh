#!/bin/sh
# Install the company-wiki binary from GitHub Releases.
#   curl -fsSL https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.sh | sh
# COMPANY_WIKI_VERSION=v0.1.0 pins a release; COMPANY_WIKI_BIN_DIR changes the folder (default ~/.local/bin).
set -eu

repo="boaz-hwang/company-wiki"
version="${COMPANY_WIKI_VERSION:-latest}"
bin_dir="${COMPANY_WIKI_BIN_DIR:-$HOME/.local/bin}"

fail() { echo "company-wiki install: $*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported OS $(uname -s); on Windows use install.ps1" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported CPU $(uname -m)" ;;
esac

asset="company-wiki_${os}_${arch}.tar.gz"
if [ "$version" = latest ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -q "$1" -O "$2"
  else fail "curl or wget is required"; fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || fail "$asset is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1); fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp" company-wiki
mkdir -p "$bin_dir"
install -m 0755 "$tmp/company-wiki" "$bin_dir/company-wiki"
echo "Installed $("$bin_dir/company-wiki" version) to $bin_dir/company-wiki"
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) echo "Add $bin_dir to PATH, for example: echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.zshrc" ;;
esac
