#!/bin/sh
set -eu

REPO="shellhaki/envi"
BINARY="envi"

fail() {
  echo "envi: $1" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "'$1' is required but not found on PATH"
}

need curl
need tar

os_raw=$(uname -s)
arch_raw=$(uname -m)

case "$os_raw" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) fail "unsupported OS: $os_raw (envi ships darwin and linux/Termux builds only)" ;;
esac

case "$arch_raw" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  armv7l | armv7 | armv6l) arch="armv7" ;;
  *) fail "unsupported architecture: $arch_raw" ;;
esac

if [ "$os" = "darwin" ] && [ "$arch" = "armv7" ]; then
  fail "no darwin/armv7 build exists"
fi

# Termux runs on Android's Linux kernel, so the plain linux build works as-is —
# this only changes where the binary lands, to Termux's own bin directory
# (already on PATH there) instead of ~/.local/bin.
install_dir="${ENVI_INSTALL_DIR:-}"
if [ -z "$install_dir" ]; then
  if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ] && echo "$PREFIX" | grep -q com.termux; then
    install_dir="$PREFIX/bin"
  else
    install_dir="$HOME/.local/bin"
  fi
fi

version="${1:-}"
if [ -z "$version" ]; then
  echo "envi: looking up the latest release..."
  tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$tag" ] || fail "couldn't determine the latest release; pass a version explicitly, e.g. install.sh v0.1.0"
else
  case "$version" in
    v*) tag="$version" ;;
    *) tag="v$version" ;;
  esac
fi
ver="${tag#v}"

archive="${BINARY}_${ver}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$tag"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "envi: downloading $archive ($tag)..."
curl -fsSL "$base_url/$archive" -o "$work/$archive" \
  || fail "download failed — check that $tag exists and has a $os/$arch build"
curl -fsSL "$base_url/checksums.txt" -o "$work/checksums.txt" \
  || fail "couldn't download checksums.txt for $tag"

echo "envi: verifying checksum..."
expected=$(grep " $archive\$" "$work/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || fail "no checksum entry found for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$work/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$work/$archive" | awk '{print $1}')
else
  fail "neither sha256sum nor shasum is available to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive — expected $expected, got $actual"

mkdir -p "$install_dir"
tar -xzf "$work/$archive" -C "$work" "$BINARY"
mv "$work/$BINARY" "$install_dir/$BINARY"
chmod +x "$install_dir/$BINARY"

echo "envi: installed $tag to $install_dir/$BINARY"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "envi: $install_dir is not on your PATH — add this to your shell profile:"
     echo "  export PATH=\"$install_dir:\$PATH\"" ;;
esac

if command -v "$install_dir/$BINARY" >/dev/null 2>&1; then
  "$install_dir/$BINARY" version
fi
