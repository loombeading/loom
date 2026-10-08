#!/usr/bin/env bash
# Copyright 2026 Takanao Endo
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

die() {
  echo "brew-formula.sh: $*" >&2
  exit 1
}

[ $# -eq 2 ] || die "usage: brew-formula.sh VERSION SHA256SUMS"
version="$1"
sums="$2"
printf '%s' "$version" | grep -Eq '^v0\.[0-9]{4}\.[0-9]+$' || die "version ${version} is not a CalVer release tag"
[ -r "$sums" ] || die "cannot read ${sums}"

sum_of() {
  awk -v f="$1" '
    ($2 == f || $2 == "*" f) && $1 ~ /^[0-9a-f]{64}$/ { s = $1; n++ }
    END { if (n != 1) exit 1; print s }
  ' "$sums" || die "${sums} has no single sha256 line for $1"
}

darwin_arm64="$(sum_of lm_darwin_arm64)"
darwin_amd64="$(sum_of lm_darwin_amd64)"
linux_arm64="$(sum_of lm_linux_arm64)"
linux_amd64="$(sum_of lm_linux_amd64)"
base="https://github.com/loombeading/loom/releases/download/${version}"

cat <<EOF
class Lm < Formula
  desc "Local task tracker for AI coding agents"
  homepage "https://github.com/loombeading/loom"
  version "${version#v}"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "${base}/lm_darwin_arm64"
      sha256 "${darwin_arm64}"
    end
    on_intel do
      url "${base}/lm_darwin_amd64"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
    on_arm do
      url "${base}/lm_linux_arm64"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "${base}/lm_linux_amd64"
      sha256 "${linux_amd64}"
    end
  end

  def install
    bin.install Dir["lm_*"].first => "lm"
  end

  test do
    assert_match "lm ${version} ", shell_output("#{bin}/lm version")
  end
end
EOF
