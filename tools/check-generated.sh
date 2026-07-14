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
  expected_generated_paths | while IFS= read -r path; do
    printf '%s  %s\n' "$(git hash-object "$path")" "$path"
  done
}

expected_generated_paths() {
  printf '%s\n' \
    gen/go/engineprotocol/codec.gen.go \
    gen/go/engineprotocol/types.gen.go \
    gen/go/protocolcommon/codec.gen.go \
    gen/go/protocolcommon/types.gen.go \
    gen/go/semantic/codec.gen.go \
    gen/go/semantic/types.gen.go \
    gen/schema-digests.json \
    packages/protocol/src/common.gen.ts \
    packages/protocol/src/engine.gen.ts \
    packages/protocol/src/semantic.gen.ts
}

actual_generated_paths() {
  {
    find gen -type f ! -path 'gen/README.md' -print
    find packages/protocol/src -type f -print
  } | LC_ALL=C sort
}

assert_expected_paths() {
  local expected actual
  expected="$(mktemp)"
  actual="$(mktemp)"
  expected_generated_paths >"$expected"
  actual_generated_paths >"$actual"
  if ! diff -u "$expected" "$actual"; then
    rm -f "$expected" "$actual"
    printf 'Generated output paths differ from the declared exact set.\n' >&2
    exit 1
  fi
  rm -f "$expected" "$actual"
}

assert_expected_paths
snapshot >"$before"
"$@"
assert_expected_paths
snapshot >"$after_first"
"$@"
assert_expected_paths
snapshot >"$after_second"

if ! diff -u "$before" "$after_first"; then
	printf 'Generated files changed. Run make generate and commit the result.\n' >&2
	exit 1
fi

if ! diff -u "$after_first" "$after_second"; then
  printf 'Two consecutive generations were not byte-identical.\n' >&2
  exit 1
fi
