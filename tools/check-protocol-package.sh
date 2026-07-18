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

unpacked="$temporary/unpacked"
mkdir -p "$unpacked"
tar -xzf "$archive" -C "$unpacked"
node --input-type=module - "$unpacked/package" <<'EOF'
import fs from "node:fs";
import path from "node:path";

const packageRoot = fs.realpathSync(process.argv[2]);
const failures = [];

function filesBelow(directory) {
  return fs.readdirSync(directory, {withFileTypes: true}).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(entryPath) : [entryPath];
  });
}

for (const mapPath of filesBelow(packageRoot).filter((entryPath) => entryPath.endsWith(".map"))) {
  const relativeMapPath = path.relative(packageRoot, mapPath);
  let map;
  try {
    map = JSON.parse(fs.readFileSync(mapPath, "utf8"));
  } catch (error) {
    failures.push(`${relativeMapPath}: invalid JSON (${error instanceof Error ? error.message : String(error)})`);
    continue;
  }

  if (!Array.isArray(map.sources) || !map.sources.every((source) => typeof source === "string")) {
    failures.push(`${relativeMapPath}: sources must be an array of strings`);
    continue;
  }
  if (map.sourceRoot !== undefined && typeof map.sourceRoot !== "string") {
    failures.push(`${relativeMapPath}: sourceRoot must be a string when present`);
    continue;
  }
  if (map.sourcesContent !== undefined &&
      (!Array.isArray(map.sourcesContent) || map.sourcesContent.length !== map.sources.length)) {
    failures.push(`${relativeMapPath}: sourcesContent must align with sources when present`);
    continue;
  }

  map.sources.forEach((source, index) => {
    if (typeof map.sourcesContent?.[index] === "string" || source.startsWith("data:")) return;

    const sourceRoot = map.sourceRoot ?? "";
    if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(sourceRoot) || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(source)) {
      failures.push(`${relativeMapPath}: ${source} is external and has no embedded sourcesContent`);
      return;
    }

    const resolvedSource = path.resolve(path.dirname(mapPath), sourceRoot, source);
    const relativeSource = path.relative(packageRoot, resolvedSource);
    if (relativeSource === "" || relativeSource.startsWith(`..${path.sep}`) || path.isAbsolute(relativeSource)) {
      failures.push(`${relativeMapPath}: ${source} resolves outside the published package`);
      return;
    }
    if (!fs.existsSync(resolvedSource) || !fs.statSync(resolvedSource).isFile()) {
      failures.push(`${relativeMapPath}: ${source} is neither published nor embedded`);
    }
  });
}

if (failures.length > 0) {
  failures.forEach((failure) => console.error(failure));
  console.error("Protocol package contains source maps with unavailable sources.");
  process.exit(1);
}
EOF

consumer="$temporary/consumer"
mkdir -p "$consumer"
printf '{"name":"layerdraw-protocol-smoke","private":true,"type":"module"}\n' >"$consumer/package.json"
corepack pnpm --dir "$consumer" add --offline --ignore-scripts "$archive" >/dev/null

(
  cd "$consumer"
  node --input-type=module -e 'await Promise.all([import("@layerdraw/protocol/common"), import("@layerdraw/protocol/semantic"), import("@layerdraw/protocol/access"), import("@layerdraw/protocol/engine"), import("@layerdraw/protocol/runtime")])'
  node --conditions=browser --input-type=module -e 'await Promise.all([import("@layerdraw/protocol/common"), import("@layerdraw/protocol/semantic"), import("@layerdraw/protocol/access"), import("@layerdraw/protocol/engine"), import("@layerdraw/protocol/runtime")])'
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
