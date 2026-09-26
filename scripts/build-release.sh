#!/usr/bin/env bash
# Builds every release binary into dist/, plus checksums.txt:
#
#   scripts/build-release.sh v1.2.3
#
# Assets are raw binaries named woffux-<os>-<arch>. install.sh and
# 'woffux update' download them, and so does every GitHub backup signer
# (releases/latest/download/woffux-linux-amd64): never rename them.
set -euo pipefail

tag="${1:?usage: scripts/build-release.sh vX.Y.Z}"
binary="woffux"
module="github.com/ngavilan-dogfy/woffux"
targets="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64"

cd "$(dirname "$0")/.."
rm -rf dist
mkdir -p dist

for target in $targets; do
	os="${target%/*}"
	arch="${target#*/}"
	out="dist/${binary}-${os}-${arch}"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X ${module}/cmd.Version=${tag}" \
		-o "$out" "./cmd/${binary}"
	echo "built ${out}"
done

cd dist
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum "${binary}"-* >checksums.txt
else
	shasum -a 256 "${binary}"-* >checksums.txt
fi
echo "wrote dist/checksums.txt"
