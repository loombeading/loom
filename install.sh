#!/usr/bin/env bash
# Copyright 2026 Takanao Endo
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

repo="loombeading/loom"
module="github.com/loombeading/loom/cmd/lm"
mode="release"
tag="latest"
ref="latest"
dest="${HOME}/bin/lm"
base_url="${LOOM_INSTALL_BASE_URL:-https://github.com/${repo}/releases}"

usage() {
  cat <<'EOF'
Usage: install.sh [--release [TAG] | --source [REF]] [--dest PATH]

  --release [TAG]  Download lm from the GitHub Release TAG (default: latest),
                   check it against SHA256SUMS and `gh attestation verify`,
                   then place it. This is the default mode.
  --source [REF]   Build lm from source with `go install <module>@REF`
                   (default: latest), then place it.
  --dest PATH      Where to place lm (default: ~/bin/lm).

Nothing is written to PATH when a download or a check fails (exit 1).
EOF
}

die() {
  echo "install.sh: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required but not found in PATH"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --release)
      mode="release"
      if [ $# -gt 1 ] && [ "${2#-}" = "$2" ]; then
        tag="$2"
        shift
      fi
      ;;
    --source)
      mode="source"
      if [ $# -gt 1 ] && [ "${2#-}" = "$2" ]; then
        ref="$2"
        shift
      fi
      ;;
    --dest)
      [ $# -gt 1 ] || die "--dest needs a path"
      dest="$2"
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 1
      ;;
  esac
  shift
done

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

place() {
  local src="$1" dir
  dir="$(dirname "$dest")"
  mkdir -p "$dir"
  chmod 0755 "$src"
  cp "$src" "${dest}.tmp.$$"
  mv -f "${dest}.tmp.$$" "$dest"
  echo "installed lm to $dest"
}

if [ "$mode" = "source" ]; then
  need go
  GOBIN="$tmp" CGO_ENABLED=0 go install -trimpath "${module}@${ref}" || die "go install ${module}@${ref} failed"
  place "$tmp/lm"
  exit 0
fi

need curl
need gh
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "sha256sum or shasum is required but not found in PATH"
fi

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac
asset="lm_${os}_${arch}"

if [ "$tag" = "latest" ]; then
  url="${base_url}/latest/download"
else
  url="${base_url}/download/${tag}"
fi

curl -fsSL -o "$tmp/$asset" "$url/$asset" || die "download failed: $url/$asset"
curl -fsSL -o "$tmp/SHA256SUMS" "$url/SHA256SUMS" || die "download failed: $url/SHA256SUMS"

want="$(awk -v a="$asset" '$2 == a || $2 == "*" a {print $1}' "$tmp/SHA256SUMS")"
[ -n "$want" ] || die "$asset is not listed in SHA256SUMS"
got="$(sha256 "$tmp/$asset")"
[ "$got" = "$want" ] || die "SHA-256 mismatch for $asset"

gh attestation verify "$tmp/$asset" --repo "$repo" || die "gh attestation verify failed for $asset"

place "$tmp/$asset"
