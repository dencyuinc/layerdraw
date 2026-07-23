#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go_command="${GO:-go}"
if [[ -z "${VERSION+x}" || -z "$VERSION" ]]; then
  printf 'VERSION must be explicitly set and nonempty\n' >&2
  exit 1
fi
version="$VERSION"
output="${ENGINE_WASM_OUTPUT_DIR:-$repo_root/dist/engine-wasm}"
allow_dirty="${ENGINE_WASM_ALLOW_DIRTY:-0}"
expected_go_version='go1.26.5'
expected_wasm_exec_sha256='0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14'

env \
  GOTOOLCHAIN=go1.26.5 \
  GOENV=off \
  GOWORK=off \
  GOEXPERIMENT= \
  GOFLAGS=-mod=readonly \
  "$go_command" run ./tools/wasmartifact validate-version -version "$version"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  source_revision="${SOURCE_REVISION:-$(git rev-parse HEAD)}"
  head_revision="$(git rev-parse HEAD)"
  if [[ "$source_revision" != "$head_revision" ]]; then
    printf 'SOURCE_REVISION must equal the clean checkout HEAD (%s)\n' "$head_revision" >&2
    exit 1
  fi
  if [[ "$allow_dirty" != '1' && -n "$(git status --porcelain --untracked-files=all)" ]]; then
    printf 'engine WASM release build requires a clean source tree\n' >&2
    git status --porcelain --untracked-files=all >&2
    exit 1
  fi
else
  source_revision="${SOURCE_REVISION:-}"
fi

if [[ ! "$source_revision" =~ ^[0-9a-f]{40}$ ]]; then
  printf 'SOURCE_REVISION must be 40 lowercase hexadecimal characters\n' >&2
  exit 1
fi

actual_go_version="$(env GOTOOLCHAIN=go1.26.5 "$go_command" version | awk '{print $3}')"
if [[ "$actual_go_version" != "$expected_go_version" ]]; then
  printf 'Go toolchain mismatch: got %s, want %s\n' "$actual_go_version" "$expected_go_version" >&2
  exit 1
fi

goroot="$(env GOTOOLCHAIN=go1.26.5 GOENV=off "$go_command" env GOROOT)"
wasm_exec="$goroot/lib/wasm/wasm_exec.js"
if [[ ! -f "$wasm_exec" ]]; then
  printf 'pinned wasm_exec.js is missing: %s\n' "$wasm_exec" >&2
  exit 1
fi
actual_wasm_exec_sha256="$(shasum -a 256 "$wasm_exec" | awk '{print $1}')"
if [[ "$actual_wasm_exec_sha256" != "$expected_wasm_exec_sha256" ]]; then
  printf 'wasm_exec.js mismatch: got %s, want %s\n' "$actual_wasm_exec_sha256" "$expected_wasm_exec_sha256" >&2
  exit 1
fi

stage="$(mktemp -d "${TMPDIR:-/tmp}/layerdraw-engine-wasm.XXXXXX")"
cleanup() {
  rm -rf "$stage"
}
trap cleanup EXIT

env \
  GOTOOLCHAIN=go1.26.5 \
  GOENV=off \
  GOWORK=off \
  GOEXPERIMENT= \
  GOFLAGS=-mod=readonly \
  "$go_command" mod verify

sbom_authority_digest="$(env \
  GOTOOLCHAIN=go1.26.5 \
  GOENV=off \
  GOWORK=off \
  GOEXPERIMENT= \
  GOFLAGS=-mod=readonly \
  "$go_command" run ./tools/wasmartifact sbom-authority -root "$repo_root" -output "$stage/engine-wasm.authority.json")"
ldflags="-buildid= -s -w -X main.releaseVersion=$version -X main.sourceRevision=$source_revision -X main.sbomAuthorityDigest=$sbom_authority_digest"
env \
  GOTOOLCHAIN=go1.26.5 \
  GOOS=js \
  GOARCH=wasm \
  CGO_ENABLED=0 \
  GOWORK=off \
  GOEXPERIMENT= \
  GOENV=off \
  GOFLAGS=-mod=readonly \
  "$go_command" build \
    -trimpath \
    -buildvcs=false \
    -ldflags="$ldflags" \
    -o "$stage/layerdraw-engine.wasm" \
    ./cmd/layerdraw-engine

cp "$wasm_exec" "$stage/wasm_exec.js"
cp "$repo_root/tests/conformance/testdata/engine_wasm_worker_v1.json" "$stage/engine-wasm-worker-v1.json"

env \
  GOTOOLCHAIN=go1.26.5 \
  GOENV=off \
  GOWORK=off \
  GOEXPERIMENT= \
  GOFLAGS=-mod=readonly \
  "$go_command" run ./tools/wasmartifact finalize \
    -root "$repo_root" \
    -output "$stage" \
    -version "$version" \
    -source-revision "$source_revision" \
    -go-license "$goroot/LICENSE"

rm -rf "$output"
mkdir -p "$(dirname "$output")"
cp -R "$stage" "$output"
printf 'Built deterministic Engine WASM artifact at %s\n' "$output"
