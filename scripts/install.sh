#!/bin/sh
# RuntimePulse installer: auto-detects OS/arch, downloads the matching
# release archive, verifies it against SHA256SUMS, and installs the
# binary. The user never picks a platform; overrides:
#   VERSION=v1.2.3   install a specific release (default: latest)
#   INSTALL_DIR=...  target directory (default: ~/.local/bin)
set -eu

REPO="tufantunc/RuntimePulse"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-}"

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    echo "error: unsupported OS '$os'." >&2
    echo "Windows: download the zip from https://github.com/$REPO/releases" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    echo "error: unsupported architecture '$arch'" >&2
    exit 1
    ;;
esac

if [ -n "$VERSION" ]; then
  base="https://github.com/$REPO/releases/download/$VERSION"
else
  base="https://github.com/$REPO/releases/latest/download"
fi
archive="runtimepulse_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading $base/$archive"
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"

verify() {
  # Fail CLOSED: capture the expected line first — macOS's native
  # sha256sum exits 0 on empty -c input, so piping a no-match grep
  # straight in would silently skip verification.
  line=$(grep " $archive\$" "$tmp/SHA256SUMS") || {
    echo "error: $archive not found in SHA256SUMS — refusing to install" >&2
    exit 1
  }
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "$line" | (cd "$tmp" && sha256sum -c - >/dev/null)
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s\n' "$line" | (cd "$tmp" && shasum -a 256 -c - >/dev/null)
  else
    echo "error: need sha256sum or shasum to verify the download" >&2
    exit 1
  fi
}
verify
echo "checksum OK"

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/runtimepulse" "$INSTALL_DIR/runtimepulse"
echo "installed $("$INSTALL_DIR/runtimepulse" --version) to $INSTALL_DIR/runtimepulse"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: $INSTALL_DIR is not on your PATH — add it to your shell profile" >&2 ;;
esac
