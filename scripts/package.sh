#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
version="${1:?Usage: bash scripts/package.sh VERSION OS ARCH}"
target_os="${2:?Target OS required}"
target_arch="${3:?Target architecture required}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
[[ "$target_os" == darwin || "$target_os" == linux ]]
[[ "$target_arch" == arm64 || "$target_arch" == amd64 ]]
package="package/${target_os}_${target_arch}"
mkdir -p "$package" dist
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
  go build -trimpath -ldflags="-s -w -X main.version=$version" \
  -o "$package/crew" ./cmd/crew
cp LICENSE NOTICE "$package/"
python3 scripts/third_party_licenses.py --output "$package/THIRD_PARTY_LICENSES.txt"
tar -czf "dist/crew_${version}_${target_os}_${target_arch}.tar.gz" \
  -C "$package" crew LICENSE NOTICE THIRD_PARTY_LICENSES.txt
