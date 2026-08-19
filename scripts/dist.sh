#!/bin/sh
# Build release binaries for every supported platform (pure Go: no cgo, no
# system deps). Output lands in dist/ with checksums.
set -e
cd "$(dirname "$0")/.."
mkdir -p dist
# dist/ is authoritative: stale artifacts from previous builds (clean or
# -dirty named) are removed here so a rebuild is ALWAYS just ./scripts/dist.sh
# with no follow-up cleanup — manual rm globs have deleted fresh binaries.
rm -f dist/leetd-* dist/checksums-*.txt
rm -rf dist/electron
VERSION="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"
# a dirty tree builds different bits than the commit it's named after, and
# stale-name cleanup has deleted fresh binaries before — mark it instead
if ! git diff --quiet 2>/dev/null; then VERSION="${VERSION}-dirty"; fi

for target in \
  darwin/arm64 darwin/amd64 \
  linux/amd64 linux/arm64 \
  windows/amd64; do
  os="${target%/*}"; arch="${target#*/}"
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  out="dist/leetd-${VERSION}-${os}-${arch}${ext}"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X leetoffice/internal/buildinfo.Version=${RELEASE:-dev} -X leetoffice/internal/buildinfo.Commit=${VERSION%-dirty} -X main.version=${RELEASE:-dev} -X main.commit=${VERSION%-dirty}" \
    -o "$out" ./cmd/leetd
  if [ "$(uname -s)" = Darwin ] && [ "$os" = darwin ]; then
    codesign --force --sign - --identifier dev.leetoffice.leetd --timestamp=none "$out" || true
  fi
  # electron-builder extraResources uses ${os}/${arch} = mac/x64, not darwin/amd64
  eb_os="$os"
  eb_arch="$arch"
  [ "$os" = "darwin" ] && eb_os=mac
  [ "$os" = "windows" ] && eb_os=win
  [ "$arch" = "amd64" ] && eb_arch=x64
  mkdir -p "dist/electron/${eb_os}-${eb_arch}"
  cp "$out" "dist/electron/${eb_os}-${eb_arch}/leetd${ext}"
done

cp docs/user-guide.html dist/leetoffice-guide.html
# shasum is a perl tool not present on every image (CI ubuntu lacked it);
# fall back through sha256sum and openssl so this works everywhere.
cksum() {
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$@"
  elif command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"
  else openssl dgst -sha256 "$@" | sed "s/^SHA2-56(.*)= //"; fi
}
( cd dist && cksum leetd-* > "checksums-${VERSION}.txt" )
echo "done:"
ls -lh dist | tail -n +2
