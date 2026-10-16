#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (c) 2026 the cautem authors
# SPDX-License-Identifier: Apache-2.0
#
# Cross-compile the linux helpers every release archive ships under
# libexec/cautem/linux-<arch>/ so installs never need Go or a checkout:
#   cautem       — proxy sidecar / supervisor relay (linux build of the CLI)
#   cautem-init  — guest init / harden
#   cautem-sshd  — guest sshd on a Unix socket (IDE / connect)
#
# Usage: scripts/build-linux-helpers.sh VERSION [OUT_DIR] [ARCH...]
# Run from cautem-cli with ../cautem-runtime checked out (GOWORK set).
set -euo pipefail

version="${1:?usage: build-linux-helpers.sh VERSION [OUT_DIR] [ARCH...]}"
out="${2:-build/helpers}"
shift $(( $# >= 2 ? 2 : 1 ))
arches=("$@")
[[ ${#arches[@]} -gt 0 ]] || arches=(amd64 arm64)

cli_dir="$(cd "$(dirname "$0")/.." && pwd)"
runtime_dir="${CAUTEM_RUNTIME_DIR:-${cli_dir}/../cautem-runtime}"
[[ -f "${runtime_dir}/go.mod" ]] || { echo "build-linux-helpers: ${runtime_dir} is not cautem-runtime" >&2; exit 1; }
mkdir -p "$out"
out="$(cd "$out" && pwd)"

ldflags="-s -w -X github.com/cautem/quarshell-cli/internal/service.BuildVersion=${version}"
for arch in "${arches[@]}"; do
  dst="${out}/linux-${arch}"
  rm -rf "$dst"
  mkdir -p "$dst"
  echo "→ linux/${arch} helpers → ${dst}"
  export GOOS=linux GOARCH="$arch" CGO_ENABLED=0
  go build -C "$cli_dir" -trimpath -ldflags="$ldflags" -o "${dst}/cautem" ./cmd/cautem
  go build -C "$runtime_dir" -trimpath -ldflags="-s -w" -o "${dst}/cautem-init" ./cmd/cautem-init
  go build -C "$runtime_dir" -trimpath -ldflags="-s -w" -o "${dst}/cautem-sshd" ./cmd/cautem-sshd
  chmod 0755 "${dst}"/*
done
