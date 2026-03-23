#!/bin/sh
set -e

REPO="adamarutyunov/launch"
BIN="launch"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  *) echo "Error: unsupported OS '$OS'"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)          ARCH="amd64" ;;
  arm64|aarch64)   ARCH="arm64" ;;
  *) echo "Error: unsupported architecture '$ARCH'"; exit 1 ;;
esac

# Resolve version
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
fi

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${BIN}_${OS}_${ARCH}.tar.gz"

echo "Installing launch ${VERSION} (${OS}/${ARCH})..."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" | tar -xz -C "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  install -m 755 "$TMP/$BIN" "$INSTALL_DIR/$BIN"
else
  sudo install -m 755 "$TMP/$BIN" "$INSTALL_DIR/$BIN"
fi

echo "Done. Run: launch"
