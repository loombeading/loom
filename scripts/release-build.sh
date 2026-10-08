#!/usr/bin/env bash
# Copyright 2026 Takanao Endo
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

go_toolchain="go1.27.1"

die() {
  echo "release-build.sh: $*" >&2
  exit 1
}

[ $# -eq 2 ] || die "usage: release-build.sh VERSION OUTDIR"
version="$1"
out="$2"
mkdir -p "$out"

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  goos="${target%/*}"
  goarch="${target#*/}"
  GOTOOLCHAIN="$go_toolchain" GOFLAGS= GOEXPERIMENT= GOAMD64=v1 GOARM64=v8.0 \
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=${version}" \
    -o "${out}/lm_${goos}_${goarch}" ./cmd/lm
done
