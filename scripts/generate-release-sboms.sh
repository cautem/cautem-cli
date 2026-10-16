#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || -z "$1" || -z "$2" ]]; then
	echo "usage: $0 <syft-binary> <dist-directory>" >&2
	exit 2
fi

syft_bin="$1"
dist_dir="$2"
if [[ ! -x "$syft_bin" ]]; then
	echo "Syft is not executable: $syft_bin" >&2
	exit 1
fi
if [[ ! -d "$dist_dir" ]]; then
	echo "release dist directory does not exist: $dist_dir" >&2
	exit 1
fi

shopt -s nullglob
archives=("$dist_dir"/*.tar.gz "$dist_dir"/*.zip)
if (( ${#archives[@]} == 0 )); then
	echo "no release archives found in $dist_dir" >&2
	exit 1
fi

for archive in "${archives[@]}"; do
	document="${archive}.spdx.json"
	"$syft_bin" "$archive" --output "spdx-json=$document" --enrich all
	jq -e '.spdxVersion == "SPDX-2.3" and (.packages | length > 0)' "$document" >/dev/null
	printf 'SBOM: %s\n' "$document"
done
