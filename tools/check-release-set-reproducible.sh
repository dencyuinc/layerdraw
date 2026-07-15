#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/layerdraw-release-set.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT

for candidate in first second; do
  RELEASE_SET_OUTPUT_DIR="$temporary/$candidate" \
    "$repository_root/tools/build-release-set.sh" >/dev/null 2>&1
done

inventory() {
  local root="$1"
  (
    cd "$root"
    find . -type f -print | LC_ALL=C sort | while IFS= read -r path; do
      printf '%s  %s\n' "$(shasum -a 256 "$path" | awk '{print $1}')" "$path"
    done
  )
}

inventory "$temporary/first" >"$temporary/first.sha256"
inventory "$temporary/second" >"$temporary/second.sha256"
if ! diff -u "$temporary/first.sha256" "$temporary/second.sha256"; then
  printf 'Fixed release-set builds are not byte reproducible.\n' >&2
  exit 1
fi

printf 'Fixed release-set builds are byte reproducible.\n'
