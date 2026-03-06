#!/bin/sh
set -e

REPO="noqcks/forja"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

detect_platform() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$os" in
    darwin) ;;
    linux) ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
  esac

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac

  echo "${os}_${arch}"
}

get_latest_version() {
  curl -sfL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"v([^"]+)".*/\1/'
}

main() {
  platform=$(detect_platform)
  version=$(get_latest_version)

  if [ -z "$version" ]; then
    echo "Error: could not determine latest version" >&2
    exit 1
  fi

  url="https://github.com/${REPO}/releases/download/v${version}/forja_${version}_${platform}.tar.gz"
  echo "Downloading forja v${version} for ${platform}..."

  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  curl -sfL "$url" | tar -xz -C "$tmpdir"

  if [ ! -w "$INSTALL_DIR" ]; then
    echo "Installing forja to ${INSTALL_DIR} (requires sudo)..."
    sudo install -m 755 "$tmpdir/forja" "$INSTALL_DIR/forja"
  else
    install -m 755 "$tmpdir/forja" "$INSTALL_DIR/forja"
  fi

  echo "forja v${version} installed to ${INSTALL_DIR}/forja"
}

main
