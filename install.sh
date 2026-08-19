#!/bin/sh
# LeetOffice one-line installer.
#
#   curl -fsSL https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/install.sh | sh
#
# (or from a checkout: sh install.sh [--version vX.Y.Z])
# Downloads the right static binary for this OS/arch, verifies its checksum,
# installs to ~/.local/bin (or a prefix you choose), and prints next steps.
# No dependencies, no root, no telemetry — the binary is ~13MB and fully
# self-contained (P1: 100% local).
set -eu

VERSION=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    *) echo "usage: install.sh [--version vX.Y.Z]" >&2; exit 1 ;;
  esac
done
# RELEASE=v0.1.0 is the env equivalent of --version (LEASE was a typo).
if [ -z "$VERSION" ]; then
  VERSION="${RELEASE:-}"
fi

# --- detect platform ---------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "unsupported OS: $OS — download a binary from the releases page" >&2; exit 1 ;;
esac
case "$ARCH" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64)  arch="amd64" ;;
  *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# --- resolve version ---------------------------------------------------------
if [ -z "$VERSION" ]; then
  if command -v curl >/dev/null 2>&1; then
    VERSION="$(curl -fsSL https://api.github.com/repos/Xtracsys/LeetOffice/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  elif command -v wget >/dev/null 2>&1; then
    VERSION="$(wget -qO- https://api.github.com/repos/Xtracsys/LeetOffice/releases/latest \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  fi
  if [ -z "$VERSION" ]; then
    echo "could not resolve the latest release — pass --version vX.Y.Z" >&2
    exit 1
  fi
fi

BASE="https://github.com/Xtracsys/LeetOffice/releases/download/${VERSION}"
NAME="leetd-${VERSION#v}-${os}-${arch}"
if [ "$os" = "windows" ]; then NAME="${NAME}.exe"; fi

# --- download to a temp dir --------------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "→ downloading ${NAME} (${VERSION})"
if command -v curl >/dev/null 2>&1; then
  curl -fSL "${BASE}/${NAME}" -o "${TMP}/${NAME}"
  curl -fSL "${BASE}/checksums-${VERSION#v}.txt" -o "${TMP}/checksums.txt"
elif command -v wget >/dev/null 2>&1; then
  wget -q "${BASE}/${NAME}" -O "${TMP}/${NAME}"
  wget -q "${BASE}/checksums-${VERSION#v}.txt" -O "${TMP}/checksums.txt"
else
  echo "need curl or wget to download" >&2; exit 1
fi

# --- verify checksum (shasum | sha256sum | openssl, same as dist.sh) ---------
line="$(grep " ${NAME}\$" "${TMP}/checksums.txt" || true)"
if [ -z "$line" ]; then
  echo "checksum for ${NAME} not in checksums.txt — not installing" >&2
  exit 1
fi
want="$(printf '%s\n' "$line" | awk '{print $1}')"
if command -v shasum >/dev/null 2>&1; then
  (cd "$TMP" && printf '%s\n' "$line" | shasum -a 256 -c -) \
    || { echo "checksum verification FAILED — not installing" >&2; exit 1; }
elif command -v sha256sum >/dev/null 2>&1; then
  (cd "$TMP" && printf '%s\n' "$line" | sha256sum -c -) \
    || { echo "checksum verification FAILED — not installing" >&2; exit 1; }
elif command -v openssl >/dev/null 2>&1; then
  got="$(openssl dgst -sha256 "${TMP}/${NAME}" | awk '{print $NF}')"
  if [ "$got" != "$want" ]; then
    echo "checksum verification FAILED — not installing" >&2
    exit 1
  fi
else
  echo "need shasum, sha256sum, or openssl to verify the download" >&2
  exit 1
fi
echo "✓ checksum verified"
chmod +x "${TMP}/${NAME}"

# --- install -----------------------------------------------------------------
PREFIX="${PREFIX:-$HOME/.local/bin}"
mkdir -p "$PREFIX"
mv "${TMP}/${NAME}" "${PREFIX}/leetd"
# macOS: a copied Go binary keeps identifier a.out; launchd SIGKILLs it
# with "Code Signature Invalid". Ad-hoc sign before the first exec.
if [ "$os" = "darwin" ]; then
  xattr -d com.apple.quarantine "${PREFIX}/leetd" 2>/dev/null || true
  xattr -d com.apple.provenance "${PREFIX}/leetd" 2>/dev/null || true
  codesign --force --sign - --identifier dev.leetoffice.leetd --timestamp=none \
    "${PREFIX}/leetd" 2>/dev/null || true
fi
echo "✓ installed ${PREFIX}/leetd"
"$PREFIX/leetd" version 2>/dev/null || true

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo
     echo "NOTE: $PREFIX is not on your PATH. Add it, then run leetd:"
     echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc   # or ~/.bashrc"
     ;;
esac

echo
echo "LeetOffice installed. Start it:"
echo "  leetd          # first run opens the setup wizard at http://127.0.0.1:7667"
echo
echo "Then click 'make always-on' in Settings to survive reboots. 100% local — welcome."
