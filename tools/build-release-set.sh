#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

output="${RELEASE_SET_OUTPUT_DIR:-$repository_root/dist/release-set}"
temporary="$(dirname "$output")/.release-set.$$.tmp"
cleanup() {
  (cd "$repository_root/packages/engine-wasm" && node tools/pack-legal.mjs clean) >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT
rm -rf "$temporary"
mkdir -p "$temporary/artifacts" "$temporary/native-legal"

version="$(node -p "require('./packages/protocol/package.json').version")"
for manifest in packages/engine-wasm/package.json packages/engine-client/package.json; do
  candidate="$(node -p "require('./$manifest').version")"
  if [[ "$candidate" != "$version" ]]; then
    printf 'Fixed release package version mismatch: %s=%s, protocol=%s\n' "$manifest" "$candidate" "$version" >&2
    exit 1
  fi
done

revision="${SOURCE_REVISION:-$(git rev-parse HEAD)}"
epoch="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$revision")}"
built_at="$(node -e 'process.stdout.write(new Date(Number(process.argv[1])*1000).toISOString().replace(".000Z","Z"))' "$epoch")"

corepack pnpm exec turbo run build

(cd packages/protocol && corepack pnpm pack --pack-destination "$temporary/artifacts" >/dev/null)
(cd packages/engine-wasm && node tools/pack-legal.mjs prepare)
(cd packages/engine-wasm && npm pack --ignore-scripts --pack-destination "$temporary/artifacts" >/dev/null)
(cd packages/engine-wasm && node tools/pack-legal.mjs clean)
(cd packages/engine-client && corepack pnpm pack --pack-destination "$temporary/artifacts" >/dev/null)

CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.releaseVersion=$version -X main.sourceRevision=$revision -X main.releaseManifestDigest=" \
  -o "$temporary/layerdraw-engine" ./cmd/layerdraw-engine

go run ./tools/licensecheck bundle \
  -binary "$temporary/layerdraw-engine" \
  -output "$temporary/native-legal" \
  -version "$version"

go run ./tools/releaseset build \
  -root "$repository_root" \
  -output "$temporary" \
  -version "$version" \
  -source-revision "$revision" \
  -built-at "$built_at" \
  -native-sbom "$temporary/native-legal/layerdraw-engine.cdx.json" \
  -native-notices "$temporary/native-legal/THIRD_PARTY_NOTICES.txt"

rm -rf "$output"
mv "$temporary" "$output"
trap - EXIT

go run ./tools/releaseset verify -root "$repository_root" -output "$output"
printf 'Built fixed release set %s at %s\n' "$version" "$output"
