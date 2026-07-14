#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

temporary=''
isolated_repository=''
dependency_root=''
repository_root=''
caller_manifest=''

write_manifest() {
  local root="$1"
  local output="$2"
  local exclude_git_admin="${3:-false}"

  node --input-type=module - "$root" "$output" "$exclude_git_admin" <<'EOF'
import crypto from "node:crypto";
import fs from "node:fs";

const root = Buffer.from(process.argv[2]);
const output = process.argv[3];
const excludeGitAdmin = process.argv[4] === "true";
const slash = Buffer.from("/");
const nul = Buffer.from([0]);
const gitName = Buffer.from(".git");
const handle = fs.openSync(output, "w");

function append(parent, name) {
  return Buffer.concat([parent, slash, name]);
}

function entryType(stats) {
  if (stats.isFile()) return "file";
  if (stats.isDirectory()) return "directory";
  if (stats.isSymbolicLink()) return "symlink";
  if (stats.isFIFO()) return "fifo";
  if (stats.isSocket()) return "socket";
  if (stats.isBlockDevice()) return "block";
  if (stats.isCharacterDevice()) return "character";
  return "unknown";
}

function writeField(value) {
  fs.writeSync(handle, value);
  fs.writeSync(handle, nul);
}

function visit(absolute, relative) {
  const entries = fs.readdirSync(absolute, {encoding: "buffer", withFileTypes: true})
    .sort((left, right) => Buffer.compare(left.name, right.name));

  for (const entry of entries) {
    if (excludeGitAdmin && relative.length === 0 && entry.name.equals(gitName)) continue;

    const absolutePath = append(absolute, entry.name);
    const relativePath = relative.length === 0 ? entry.name : append(relative, entry.name);
    const stats = fs.lstatSync(absolutePath);
    const type = entryType(stats);
    let payload = Buffer.alloc(0);

    if (type === "file") {
      payload = Buffer.from(crypto.createHash("sha256").update(fs.readFileSync(absolutePath)).digest("hex"));
    } else if (type === "symlink") {
      payload = fs.readlinkSync(absolutePath, {encoding: "buffer"});
    } else if (type === "block" || type === "character") {
      payload = Buffer.from(String(stats.rdev));
    }

    writeField(Buffer.from(type));
    writeField(Buffer.from((stats.mode & 0o7777).toString(8).padStart(4, "0")));
    writeField(relativePath);
    writeField(payload);

    if (type === "directory") visit(absolutePath, relativePath);
  }
}

try {
  visit(root, Buffer.alloc(0));
} finally {
  fs.closeSync(handle);
}
EOF
}

cleanup() {
  local status=$?
  local current_manifest
  trap - EXIT

  if [[ -n "$caller_manifest" && -n "$repository_root" && -d "$repository_root" ]]; then
    current_manifest="$temporary/caller-current.manifest"
    if ! write_manifest "$repository_root" "$current_manifest" true; then
      printf 'Could not verify caller preservation during generation-gate cleanup.\n' >&2
      status=1
    elif ! cmp -s "$caller_manifest" "$current_manifest"; then
      printf 'Generation gate disturbed the caller working tree or dependency state.\n' >&2
      status=1
    fi
  fi

  if [[ -n "$temporary" ]] && ! rm -rf "$temporary"; then
    printf 'Generation gate could not remove its temporary isolation directory.\n' >&2
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT

make_temporary_directory() {
  local parent="${TMPDIR:-/tmp}"
  mkdir -p "$parent"
  mktemp -d "$parent/layerdraw-generate-check.XXXXXX"
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
  expected="$temporary/expected-generated-paths"
  actual="$temporary/actual-generated-paths"
  expected_generated_paths >"$expected"
  actual_generated_paths >"$actual"
  if ! diff -u "$expected" "$actual"; then
    printf 'Generated output paths differ from the declared exact set.\n' >&2
    return 1
  fi
}

compare_manifests() {
  local before="$1"
  local after="$2"
  local phase="$3"
  local scope="$4"
  local allowlist="$temporary/expected-generated-paths"

  node --input-type=module - "$before" "$after" "$phase" "$scope" "$allowlist" <<'EOF'
import fs from "node:fs";

const [beforePath, afterPath, phase, scope, allowlistPath] = process.argv.slice(2);

function parseManifest(filename) {
  const input = fs.readFileSync(filename);
  const fields = [];
  let start = 0;
  for (let index = 0; index < input.length; index += 1) {
    if (input[index] !== 0) continue;
    fields.push(input.subarray(start, index));
    start = index + 1;
  }
  if (start !== input.length || fields.length % 4 !== 0) {
    throw new Error(`invalid filesystem manifest: ${filename}`);
  }

  const entries = new Map();
  for (let index = 0; index < fields.length; index += 4) {
    const path = fields[index + 2];
    entries.set(path.toString("base64"), {
      path,
      signature: Buffer.concat([
        fields[index], Buffer.from([0]), fields[index + 1], Buffer.from([0]), fields[index + 3],
      ]),
    });
  }
  return entries;
}

function displayPath(path) {
  const decoded = path.toString("utf8");
  if (Buffer.from(decoded).equals(path) && !/[\u0000-\u001f\u007f]/u.test(decoded)) return decoded;
  return JSON.stringify(decoded);
}

const before = parseManifest(beforePath);
const after = parseManifest(afterPath);
const keys = new Set([...before.keys(), ...after.keys()]);
const changed = [...keys]
  .filter((key) => {
    const left = before.get(key);
    const right = after.get(key);
    return left === undefined || right === undefined || !left.signature.equals(right.signature);
  })
  .map((key) => before.get(key)?.path ?? after.get(key).path)
  .sort(Buffer.compare);

if (changed.length === 0) process.exit(0);

for (const path of changed) console.error(displayPath(path));

if (scope === "dependencies") {
  console.error(`Generation ${phase} changed the isolated dependency snapshot.`);
  process.exit(1);
}

const allowlist = new Set(
  fs.readFileSync(allowlistPath, "utf8").split("\n").filter(Boolean).map((path) => Buffer.from(path).toString("base64")),
);
const unexpected = changed.some((path) => !allowlist.has(path.toString("base64")));
if (unexpected) {
  console.error(`Generation ${phase} changed paths outside the declared exact generated-output set.`);
} else {
  console.error(`Generated files changed during generation ${phase}. Run make generate and commit the result.`);
}
process.exit(1);
EOF
}

validate_dependency_snapshot() {
  node --input-type=module - "$repository_root" "$dependency_root" <<'EOF'
import fs from "node:fs";

const callerRoot = Buffer.from(process.argv[2]);
const dependencyRoot = Buffer.from(process.argv[3]);
const slash = Buffer.from("/");
const dependencyReal = fs.realpathSync(dependencyRoot, {encoding: "buffer"});
const dependencyPrefix = Buffer.concat([dependencyReal, slash]);

function append(parent, name) {
  return Buffer.concat([parent, slash, name]);
}

function isWithinDependencyRoot(candidate) {
  return candidate.equals(dependencyReal) ||
    (candidate.length > dependencyPrefix.length && candidate.subarray(0, dependencyPrefix.length).equals(dependencyPrefix));
}

function visit(absolute, relative) {
  const entries = fs.readdirSync(absolute, {encoding: "buffer", withFileTypes: true});
  for (const entry of entries) {
    const absolutePath = append(absolute, entry.name);
    const relativePath = relative.length === 0 ? entry.name : append(relative, entry.name);
    const stats = fs.lstatSync(absolutePath, {bigint: true});

    if (stats.isSymbolicLink()) {
      let resolved;
      try {
        resolved = fs.realpathSync(absolutePath, {encoding: "buffer"});
      } catch {
        throw new Error(`isolated dependency snapshot contains a broken symlink: ${relativePath.toString("utf8")}`);
      }
      if (!isWithinDependencyRoot(resolved)) {
        throw new Error(`isolated dependency snapshot exposes an external symlink: ${relativePath.toString("utf8")}`);
      }
    } else if (stats.isFile()) {
      const callerPath = append(callerRoot, relativePath);
      if (fs.existsSync(callerPath)) {
        const callerStats = fs.lstatSync(callerPath, {bigint: true});
        if (callerStats.isFile() && stats.dev === callerStats.dev && stats.ino === callerStats.ino) {
          throw new Error(`isolated dependency snapshot shares a file inode with the caller: ${relativePath.toString("utf8")}`);
        }
      }
    } else if (stats.isDirectory()) {
      visit(absolutePath, relativePath);
    }
  }
}

visit(dependencyRoot, Buffer.alloc(0));
EOF
}

materialize_dependency_snapshot() {
  local source relative relative_parent destination dependency_entry entry_name pnpm_store dependency_workspace
  dependency_root="$temporary/dependencies"
  dependency_workspace="$temporary/dependency-workspace"
  mkdir -p "$dependency_root" "$dependency_workspace"

  if [[ -f "$isolated_repository/pnpm-lock.yaml" && -f "$isolated_repository/pnpm-workspace.yaml" ]]; then
    # Install in a separate staging tree so dependencies never occupy the
    # repository that runs generation. Copy imports prevent store hardlinks.
    git -C "$isolated_repository" archive --format=tar HEAD | tar -xf - -C "$dependency_workspace"
    pnpm_store="$(corepack pnpm store path)"
    (
      cd "$dependency_workspace"
      CI=true corepack pnpm install \
        --offline \
        --frozen-lockfile \
        --frozen-store \
        --ignore-scripts \
        --package-import-method=copy \
        --store-dir "$pnpm_store"
    )
  else
    while IFS= read -r -d '' source; do
      relative="${source#"$repository_root"/}"
      relative_parent="${relative%/*}"
      if [[ "$relative_parent" == "$relative" ]]; then
        relative_parent=''
      fi
      mkdir -p "$dependency_workspace/$relative_parent"
      cp -R -P "$source" "$dependency_workspace/$relative"
    done < <(
      find "$repository_root" \
        -path "$repository_root/.git" -prune -o \
        -name node_modules -prune -print0
    )
  fi

  while IFS= read -r -d '' source; do
    relative="${source#"$dependency_workspace"/}"
    relative_parent="${relative%/*}"
    if [[ "$relative_parent" == "$relative" ]]; then
      relative_parent=''
    fi
    destination="$dependency_root/$relative"
    mkdir -p "$dependency_root/$relative_parent"
    mv "$source" "$destination"
    mkdir "$isolated_repository/$relative"
    while IFS= read -r -d '' dependency_entry; do
      entry_name="${dependency_entry##*/}"
      if [[ -L "$dependency_entry" ]]; then
        cp -P "$dependency_entry" "$isolated_repository/$relative/$entry_name"
      else
        ln -s "$dependency_entry" "$isolated_repository/$relative/$entry_name"
      fi
    done < <(find "$destination" -mindepth 1 -maxdepth 1 -print0)
  done < <(
    find "$dependency_workspace" \
      -name node_modules -prune -print0
  )

  validate_dependency_snapshot
}

fixture_output=''
fixture_status=0

invoke_fixture_gate() {
  local fixture="$1"
  local command="$2"
  local before after worktrees_before worktrees_after gate_temporary
  before="$(mktemp "$temporary/fixture-before.XXXXXX")"
  after="$(mktemp "$temporary/fixture-after.XXXXXX")"
  gate_temporary="$(mktemp -d "$temporary/fixture-gate.XXXXXX")"
  worktrees_before="$(git -C "$fixture" worktree list --porcelain)"
  write_manifest "$fixture" "$before" true

  if fixture_output="$(cd "$fixture" && TMPDIR="$gate_temporary" ./tools/check-generated.sh bash -c "$command" 2>&1)"; then
    fixture_status=0
  else
    fixture_status=$?
  fi

  write_manifest "$fixture" "$after" true
  worktrees_after="$(git -C "$fixture" worktree list --porcelain)"
  if ! cmp -s "$before" "$after"; then
    printf '%s\n' "$fixture_output" >&2
    printf 'Generation gate self-test did not preserve all caller filesystem bytes and types.\n' >&2
    return 1
  fi
  if [[ "$worktrees_before" != "$worktrees_after" ]]; then
    printf 'Generation gate self-test changed the caller worktree registry.\n' >&2
    return 1
  fi
  if [[ -n "$(find "$gate_temporary" -mindepth 1 -print -quit)" ]]; then
    printf 'Generation gate self-test left its isolation directory behind.\n' >&2
    return 1
  fi
}

assert_fixture_accepts() {
  local fixture="$1"
  local command="$2"
  invoke_fixture_gate "$fixture" "$command"
  if (( fixture_status != 0 )); then
    printf '%s\n' "$fixture_output" >&2
    printf 'Generation gate self-test rejected a clean or fully cleaned generation.\n' >&2
    return 1
  fi
}

assert_fixture_rejects() {
  local fixture="$1"
  local command="$2"
  local expected_path="${3:-}"
  invoke_fixture_gate "$fixture" "$command"
  if (( fixture_status == 0 )); then
    printf 'Generation gate self-test accepted an unexpected filesystem change.\n' >&2
    return 1
  fi
  if [[ -n "$expected_path" ]] && ! grep -Fxq -- "$expected_path" <<<"$fixture_output"; then
    printf '%s\n' "$fixture_output" >&2
    printf 'Generation gate self-test did not report %s.\n' "$expected_path" >&2
    return 1
  fi
}

run_self_test() {
  local fixture path pass_counter
  repository_root=''
  caller_manifest=''
  temporary="$(make_temporary_directory)"
  fixture="$temporary/repository"
  mkdir -p "$fixture/tools"
  cp "${BASH_SOURCE[0]}" "$fixture/tools/check-generated.sh"
  chmod +x "$fixture/tools/check-generated.sh"

  while IFS= read -r path; do
    mkdir -p "$fixture/$(dirname "$path")"
    printf 'generated baseline\n' >"$fixture/$path"
  done < <(expected_generated_paths)
  printf 'generated files only\n' >"$fixture/gen/README.md"
  printf 'tracked baseline\n' >"$fixture/tracked-outside.txt"
  printf 'caller baseline\n' >"$fixture/caller.txt"
  printf 'deleted caller baseline\n' >"$fixture/caller-deleted.txt"
  printf 'ignored-output/\nnode_modules/\ncaller-ignored/\n' >"$fixture/.gitignore"

  git -C "$fixture" init -q
  git -C "$fixture" config user.email 'generation-gate-self-test@layerdraw.invalid'
  git -C "$fixture" config user.name 'LayerDraw generation gate self-test'
  git -C "$fixture" add .
  git -C "$fixture" commit -qm 'test: initialize generation gate fixture'

  printf 'caller change\n' >"$fixture/caller.txt"
  printf 'caller untracked\n' >"$fixture/caller-untracked.txt"
  rm "$fixture/caller-deleted.txt"
  ln -s caller-untracked.txt "$fixture/caller-link"
  mkdir -p "$fixture/caller-ignored" "$fixture/node_modules/review-sentinel-dir"
  printf 'caller ignored dirt\n' >"$fixture/caller-ignored/value.txt"
  printf 'caller dependency dirt\n' >"$fixture/node_modules/review-sentinel-dir/caller.txt"

  assert_fixture_accepts "$fixture" ':'
  assert_fixture_accepts "$fixture" 'mkdir -p ignored-output && : > ignored-output/temporary.txt && rm -rf ignored-output'
  assert_fixture_accepts "$fixture" \
    "printf 'temporary dependency write\\n' > node_modules/review-sentinel-dir/caller.txt; printf 'caller dependency dirt\\n' > node_modules/review-sentinel-dir/caller.txt"
  assert_fixture_rejects "$fixture" 'touch unexpected-output.txt' 'unexpected-output.txt'
  assert_fixture_rejects "$fixture" 'mkdir -p unexpected/nested && touch unexpected/nested/output.txt' 'unexpected/nested/output.txt'
  assert_fixture_rejects "$fixture" 'mkdir unexpected-empty-directory' 'unexpected-empty-directory'
  assert_fixture_rejects "$fixture" 'mkdir -p ignored-output && touch ignored-output/result.txt' 'ignored-output/result.txt'
  assert_fixture_rejects "$fixture" ': > tracked-outside.txt' 'tracked-outside.txt'
  assert_fixture_rejects "$fixture" 'chmod u+x tracked-outside.txt' 'tracked-outside.txt'
  assert_fixture_rejects "$fixture" 'rm tracked-outside.txt' 'tracked-outside.txt'
  assert_fixture_rejects "$fixture" 'ln -s tracked-outside.txt unexpected-link' 'unexpected-link'
  assert_fixture_rejects "$fixture" 'rm tracked-outside.txt && ln -s caller.txt tracked-outside.txt' 'tracked-outside.txt'
  assert_fixture_rejects "$fixture" 'touch "$(printf "unexpected\\noutput.txt")"'
  assert_fixture_rejects "$fixture" ': > gen/schema-digests.json' 'gen/schema-digests.json'
  assert_fixture_rejects "$fixture" \
    "printf 'generator overwrote dependency dirt\\n' > node_modules/review-sentinel-dir/caller.txt" \
    'node_modules/review-sentinel-dir/caller.txt'

  pass_counter="$temporary/pass-counter"
  export GENERATION_GATE_SELF_TEST_COUNTER="$pass_counter"
  assert_fixture_rejects "$fixture" \
    'count=0; if [[ -f "$GENERATION_GATE_SELF_TEST_COUNTER" ]]; then count="$(<"$GENERATION_GATE_SELF_TEST_COUNTER")"; fi; count=$((count + 1)); printf "%s\n" "$count" >"$GENERATION_GATE_SELF_TEST_COUNTER"; if (( count == 2 )); then touch pass-two-output.txt; fi' \
    'pass-two-output.txt'
  unset GENERATION_GATE_SELF_TEST_COUNTER
  if ! grep -Fq 'Generation pass 2' <<<"$fixture_output"; then
    printf '%s\n' "$fixture_output" >&2
    printf 'Generation gate self-test did not identify pass-2-only drift.\n' >&2
    return 1
  fi

  printf 'Generation gate self-tests passed.\n'
}

if [[ "${1:-}" == '--self-test' ]]; then
  if (( $# != 1 )); then
    printf 'usage: %s --self-test | <generate command> [args...]\n' "$0" >&2
    exit 2
  fi
  run_self_test
  exit $?
fi

if (( $# == 0 )); then
  printf 'usage: %s --self-test | <generate command> [args...]\n' "$0" >&2
  exit 2
fi

repository_root="$(git rev-parse --show-toplevel)"
temporary="$(make_temporary_directory)"
isolated_repository="$temporary/repository"
caller_manifest="$temporary/caller-before.manifest"
write_manifest "$repository_root" "$caller_manifest" true

revision="$(git -C "$repository_root" rev-parse HEAD)"
git clone --quiet --no-local --no-hardlinks --no-checkout "$repository_root" "$isolated_repository"
git -C "$isolated_repository" checkout --quiet --detach "$revision"
git -C "$isolated_repository" remote remove origin
materialize_dependency_snapshot

repository_baseline="$temporary/repository-baseline.manifest"
dependency_baseline="$temporary/dependency-baseline.manifest"
repository_current="$temporary/repository-current.manifest"
dependency_current="$temporary/dependency-current.manifest"
mkdir -p "$temporary/cache/turbo" "$temporary/cache/go-build" "$temporary/runtime-tmp"

cd "$isolated_repository"
assert_expected_paths
write_manifest "$isolated_repository" "$repository_baseline" true
write_manifest "$dependency_root" "$dependency_baseline"

for pass in 1 2; do
  # The frozen install above is the dependency check. pnpm's default pre-exec
  # check rewrites link metadata, so disable that redundant mutating step.
  TMPDIR="$temporary/runtime-tmp" \
    XDG_CACHE_HOME="$temporary/cache/xdg" \
    TURBO_CACHE_DIR="$temporary/cache/turbo" \
    TURBO_TELEMETRY_DISABLED=1 \
    GOCACHE="$temporary/cache/go-build" \
    GOTMPDIR="$temporary/runtime-tmp" \
    npm_config_store_dir="$temporary/cache/pnpm-store" \
    pnpm_config_verify_deps_before_run=false \
    "$@"
  assert_expected_paths
  write_manifest "$isolated_repository" "$repository_current" true
  write_manifest "$dependency_root" "$dependency_current"
  compare_manifests "$repository_baseline" "$repository_current" "pass $pass" repository
  compare_manifests "$dependency_baseline" "$dependency_current" "pass $pass" dependencies
done
