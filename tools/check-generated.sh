#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

if (( $# == 0 )); then
  printf 'usage: %s <generate command> [args...]\n' "$0" >&2
  exit 2
fi

before="$(mktemp)"
after_first="$(mktemp)"
after_second="$(mktemp)"
trap 'rm -f "$before" "$after_first" "$after_second"' EXIT

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
snapshot >"$after_first"
"$@"
snapshot >"$after_second"

if ! diff -u "$before" "$after_first"; then
	printf 'Generated files changed. Run make generate and commit the result.\n' >&2
	exit 1
fi

if ! diff -u "$after_first" "$after_second"; then
  printf 'Two consecutive generations were not byte-identical.\n' >&2
  exit 1
fi
