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

native_dir="$(./tools/install-ladybug-native.sh)"
native_export_flags=""
if [[ "$(uname -s)" == "Linux" ]]; then native_export_flags="-Wl,--export-dynamic"; fi
mkdir -p "$temporary/desktop-native-stage" "$temporary/desktop-native-legal"
CGO_ENABLED=1 CGO_CFLAGS="-I$native_dir ${CGO_CFLAGS:-}" CGO_LDFLAGS="-L$native_dir $native_export_flags ${CGO_LDFLAGS:-}" \
  go build -trimpath -buildvcs=false -tags ladybug_native \
  -ldflags "-buildid= -s -w -X main.releaseVersion=$version -X main.sourceRevision=$revision -X main.releaseManifestDigest=" \
  -o "$temporary/desktop-native-stage/layerdraw-host-native" ./cmd/layerdraw-host
cp "$native_dir/libfts.lbug_extension" "$temporary/desktop-native-stage/libfts.lbug_extension"
cp "$native_dir/libvector.lbug_extension" "$temporary/desktop-native-stage/libvector.lbug_extension"
cp "$native_dir/libalgo.lbug_extension" "$temporary/desktop-native-stage/libalgo.lbug_extension"
fts_digest="$(node -e "const fs=require('node:fs'),c=require('node:crypto');process.stdout.write(c.createHash('sha256').update(fs.readFileSync(process.argv[1])).digest('hex'))" "$native_dir/libfts.lbug_extension")"
vector_digest="$(node -e "const fs=require('node:fs'),c=require('node:crypto');process.stdout.write(c.createHash('sha256').update(fs.readFileSync(process.argv[1])).digest('hex'))" "$native_dir/libvector.lbug_extension")"
algo_digest="$(node -e "const fs=require('node:fs'),c=require('node:crypto');process.stdout.write(c.createHash('sha256').update(fs.readFileSync(process.argv[1])).digest('hex'))" "$native_dir/libalgo.lbug_extension")"
native_platform="$(go env GOOS)/$(go env GOARCH)"
printf '{"ladybug_version":"0.17.0","platform":"%s","fts_extension":"libfts.lbug_extension","fts_sha256":"%s","vector_extension":"libvector.lbug_extension","vector_sha256":"%s","algo_extension":"libalgo.lbug_extension","algo_sha256":"%s","host":"layerdraw-host-native"}\n' "$native_platform" "$fts_digest" "$vector_digest" "$algo_digest" \
  > "$temporary/desktop-native-stage/ladybug-native.json"
go run ./tools/licensecheck bundle \
  -binary "$temporary/desktop-native-stage/layerdraw-host-native" \
  -output "$temporary/desktop-native-legal" \
  -version "$version" \
  -bundled-component "LadybugDB FTS extension|0.17.0|$native_dir/libfts.lbug_extension|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0" \
  -bundled-component "LadybugDB Vector extension|0.17.0|$native_dir/libvector.lbug_extension|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0" \
  -bundled-component "LadybugDB Algo extension|0.17.0|$native_dir/libalgo.lbug_extension|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0"
cp "$temporary/desktop-native-legal/LICENSE" \
  "$temporary/desktop-native-legal/NOTICE" \
  "$temporary/desktop-native-legal/LICENSING.md" \
  "$temporary/desktop-native-legal/THIRD_PARTY_NOTICES.txt" \
  "$temporary/desktop-native-stage/"
node - "$temporary/desktop-native-stage" "$epoch" <<'NODE'
const fs = require('node:fs');
const path = require('node:path');
const root = process.argv[2];
const epoch = Number(process.argv[3]);
for (const name of fs.readdirSync(root)) fs.utimesSync(path.join(root, name), epoch, epoch);
NODE
(cd "$temporary/desktop-native-stage" && find . -type f -print | sed 's#^\./##' | LC_ALL=C sort | COPYFILE_DISABLE=1 tar -cf - -T -) | gzip -n > "$temporary/artifacts/layerdraw-host-native-$version.tar.gz"

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
