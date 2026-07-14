#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
  printf 'reproducibility comparison requires a clean source tree\n' >&2
  exit 1
fi

source_revision="${SOURCE_REVISION:-$(git rev-parse HEAD)}"
version="${VERSION:-0.0.0-dev}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/layerdraw-engine-wasm-repro.XXXXXX")"
cleanup() {
  rm -rf "$temporary"
}
trap cleanup EXIT

for build in one two; do
  root="$temporary/$build/source"
  output="$temporary/$build/output"
  mkdir -p "$root"
  git archive --format=tar "$source_revision" | tar -xf - -C "$root"
  (
    cd "$root"
    VERSION="$version" \
      SOURCE_REVISION="$source_revision" \
      ENGINE_WASM_ALLOW_DIRTY=1 \
      ENGINE_WASM_OUTPUT_DIR="$output" \
      ./tools/build-engine-wasm.sh
  )
done

if ! diff -ru "$temporary/one/output" "$temporary/two/output"; then
  printf 'isolated Engine WASM builds are not byte-identical\n' >&2
  exit 1
fi

find "$temporary/one/output" -type f -print0 \
  | sort -z \
  | xargs -0 shasum -a 256 \
  | sed "s#$temporary/one/output/##"
printf 'Two isolated Engine WASM builds are byte-identical.\n'
