#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repository_root="$(pwd)"
package_dir="$repository_root/packages/protocol"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

pack_result="$(cd "$package_dir" && corepack pnpm pack --pack-destination "$temporary" --json)"
archive_name="$(node -e 'const fs=require("node:fs");const value=JSON.parse(fs.readFileSync(0,"utf8"));process.stdout.write(value.filename)' <<<"$pack_result")"
if [[ "$archive_name" = /* ]]; then
  archive="$archive_name"
else
  archive="$temporary/$archive_name"
fi

if [[ ! -f "$archive" ]]; then
  printf 'Protocol package archive was not created: %s\n' "$archive" >&2
  exit 1
fi
if tar -tzf "$archive" | LC_ALL=C sort | rg '(\.tsbuildinfo$|/src/|/test/)' >/dev/null; then
  printf 'Protocol package contains build metadata or unpublished source/test files.\n' >&2
  exit 1
fi

consumer="$temporary/consumer"
mkdir -p "$consumer"
printf '{"name":"layerdraw-protocol-smoke","private":true,"type":"module"}\n' >"$consumer/package.json"
corepack pnpm --dir "$consumer" add --offline --ignore-scripts "$archive" >/dev/null

(
  cd "$consumer"
  node --input-type=module -e 'await Promise.all([import("@layerdraw/protocol/common"), import("@layerdraw/protocol/semantic"), import("@layerdraw/protocol/engine")])'
  node --conditions=browser --input-type=module -e 'await Promise.all([import("@layerdraw/protocol/common"), import("@layerdraw/protocol/semantic"), import("@layerdraw/protocol/engine")])'
  node --input-type=module - <<'EOF'
for (const path of ["@layerdraw/protocol", "@layerdraw/protocol/dist/common.gen.js"]) {
  try {
    await import(path);
    throw new Error(`unexpected package export ${path}`);
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("unexpected package export")) throw error;
    if (!(error && typeof error === "object" && "code" in error && error.code === "ERR_PACKAGE_PATH_NOT_EXPORTED")) throw error;
  }
}
EOF
)
