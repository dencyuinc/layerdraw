#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

if (( $# == 0 )); then
  printf 'usage: %s <generate command> [args...]\n' "$0" >&2
  exit 2
fi

before="$(mktemp)"
after="$(mktemp)"
trap 'rm -f "$before" "$after"' EXIT

snapshot() {
  local root

  for root in gen packages/protocol; do
    if [[ -d "$root" ]]; then
      find "$root" -type f -not -path '*/node_modules/*' -print
    fi
  done | LC_ALL=C sort | while IFS= read -r path; do
    printf '%s  %s\n' "$(git hash-object "$path")" "$path"
  done
}

snapshot >"$before"
"$@"
snapshot >"$after"

if ! diff -u "$before" "$after"; then
  printf 'Generated files changed. Run make generate and commit the result.\n' >&2
  exit 1
fi
