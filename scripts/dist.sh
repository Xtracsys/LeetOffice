#!/bin/sh
# Build release binaries for every supported platform (pure Go: no cgo, no
# system deps). Output lands in dist/ with checksums.
set -e
cd "$(dirname "$0")/.."
mkdir -p dist
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
    -ldflags "-s -w -X main.version=${RELEASE:-dev} -X main.commit=${VERSION%-dirty}" \
    -o "$out" ./cmd/leetd
done

cp docs/user-guide.html dist/leetoffice-guide.html
( cd dist && shasum -a 256 leetd-* > "checksums-${VERSION}.txt" )
echo "done:"
ls -lh dist | tail -n +2
