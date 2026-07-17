#!/bin/sh
# floe installer — https://github.com/Jszigeti/floe
set -eu

REPO="Jszigeti/floe"
INSTALL_DIR="${FLOE_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) ;;
  *) echo "floe only supports macOS" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64) ARCH="arm64" ;;
  x86_64) ARCH="amd64" ;;
  *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
[ -n "$TAG" ] || { echo "could not resolve latest release" >&2; exit 1; }

URL="https://github.com/$REPO/releases/download/$TAG/floe_${TAG#v}_darwin_${ARCH}.tar.gz"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

UPGRADE=false
[ -e "$INSTALL_DIR/floe" ] && UPGRADE=true

curl -fsSL "$URL" | tar -xz -C "$TMP"
mkdir -p "$INSTALL_DIR"
mv "$TMP/floe" "$INSTALL_DIR/floe"
chmod +x "$INSTALL_DIR/floe"

echo "✓ floe $TAG installed to $INSTALL_DIR/floe"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add it to your PATH: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
echo "Get started: floe init"

if [ "$UPGRADE" = true ]; then
  echo ""
  echo "Note: macOS ties Full Disk Access to the exact binary, so this upgrade cleared it."
  echo "If you use the daily schedule, re-grant it: System Settings → Privacy & Security →"
  echo "Full Disk Access → add $INSTALL_DIR/floe. Check anytime with: floe doctor"
fi
