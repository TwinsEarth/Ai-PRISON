#!/usr/bin/env bash
# Build all distributable release archives into dist/.
#
# Usage:   scripts/build-release.sh [VERSION]
# Env:     COMMIT, BUILD_TIME (optional overrides)
#
# Produces:
#   dist/aiprison_linux_amd64.tar.gz,  aiprison_linux_arm64.tar.gz
#   dist/aiprison_darwin_amd64.tar.gz,aiprison_darwin_arm64.tar.gz
#   dist/aiprison_windows_amd64.zip,   aiprison_windows_arm64.zip
#   dist/checksums.txt  (sha256, "<64hex>  <basename>")
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-${VERSION:-1.0.0}}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

# package main's linker symbol prefix is `main.*` (verified via go tool nm).
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"

rm -rf dist staging
mkdir -p dist staging
# Always remove the staging tree, even on a mid-script failure, so no residue
# is left in the repository working tree (dist/ is git-ignored).
trap 'rm -rf staging' EXIT

TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

for t in ${TARGETS}; do
  os="${t%/*}"
  arch="${t#*/}"
  name="aiprison_${os}_${arch}"
  stage="staging/${name}"
  mkdir -p "${stage}"

  if [ "${os}" = "windows" ]; then
    out="${stage}/aiprison.exe"
  else
    out="${stage}/aiprison"
  fi

  echo ">> building ${os}/${arch} -> ${out}"
  GOOS="${os}" GOARCH="${arch}" CGO_ENABLED=0 \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${out}" ./cmd/aiprison

  cp LICENSE README.md "${stage}/"

  if [ "${os}" = "windows" ]; then
    if command -v zip >/dev/null 2>&1; then
      (cd "${stage}" && zip -q "../../dist/${name}.zip" aiprison.exe LICENSE README.md)
    else
      # macOS hosts without the `zip` binary: use the stdlib zipfile module.
      python3 - "${name}" "${stage}" <<'PY'
import os, sys, zipfile
name, stage = sys.argv[1], sys.argv[2]
out = os.path.join("dist", name + ".zip")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for f in ("aiprison.exe", "LICENSE", "README.md"):
        z.write(os.path.join(stage, f), f)
PY
    fi
  else
    tar -czf "dist/${name}.tar.gz" -C "${stage}" aiprison LICENSE README.md
  fi
done

rm -rf staging

# checksums.txt: "<64hex>  <basename>" (shasum emits the two-space format).
( cd dist && shasum -a 256 *.tar.gz *.zip > checksums.txt )

echo ">> dist/ contents:"
ls -l dist
echo ">> checksums.txt:"
cat dist/checksums.txt
