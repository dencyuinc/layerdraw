#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

temporary=''
isolated_repository=''
dependency_root=''
repository_root=''
caller_manifest=''
package_manager_root=''
package_manager_home=''
package_manager_bin=''
package_manager_baseline=''
package_manager_node=''
package_manager_corepack=''
pinned_pnpm_version='11.12.0'

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

  if [[ -n "$temporary" && -d "$temporary" ]]; then
    chmod -R u+w "$temporary" 2>/dev/null || true
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
    gen/go/accessprotocol/codec.gen.go \
    gen/go/accessprotocol/types.gen.go \
    gen/go/engineprotocol/codec.gen.go \
    gen/go/engineprotocol/types.gen.go \
    gen/go/protocolcommon/codec.gen.go \
    gen/go/protocolcommon/types.gen.go \
    gen/go/runtimeprotocol/codec.gen.go \
    gen/go/runtimeprotocol/types.gen.go \
    gen/go/semantic/codec.gen.go \
    gen/go/semantic/types.gen.go \
    gen/schema-digests.json \
    packages/protocol/src/access.gen.ts \
    packages/protocol/src/common.gen.ts \
    packages/protocol/src/engine.gen.ts \
    packages/protocol/src/runtime.gen.ts \
    packages/protocol/src/semantic.gen.ts \
    tests/conformance/testdata/engine_compile_parity_v1.json
}

actual_generated_paths() {
  {
    find gen -type f ! -path 'gen/README.md' -print
    find packages/protocol/src -type f -print
    printf '%s\n' tests/conformance/testdata/engine_compile_parity_v1.json
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
if (scope === "package-manager") {
  console.error(`Generation ${phase} changed the preserved package-manager bundle.`);
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

read_pinned_package_manager() {
  node --input-type=module - "$isolated_repository/package.json" "$pinned_pnpm_version" <<'EOF'
import fs from "node:fs";

const [filename, expectedVersion] = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync(filename, "utf8"));
const expected = `pnpm@${expectedVersion}`;
if (manifest.packageManager !== expected) {
  console.error(`package.json must pin packageManager to ${expected}`);
  process.exit(1);
}
process.stdout.write(manifest.packageManager);
EOF
}

run_pinned_pnpm() {
  COREPACK_ENABLE_NETWORK=0 \
    COREPACK_HOME="$package_manager_home" \
    NODE_DISABLE_COMPILE_CACHE=1 \
    "$package_manager_node" "$package_manager_corepack" pnpm "$@"
}

materialize_package_manager() {
  local package_manager_spec corepack_executable corepack_source corepack_source_directory
  local corepack_package_root corepack_relative_entry
  local candidate actual_version package_version
  local package_manager_archive package_manager_log
  local -a corepack_homes=()

  if [[ ! -f "$isolated_repository/package.json" ]]; then
    return
  fi
  package_manager_spec="$(read_pinned_package_manager)"
  package_manager_node="$(command -v node)"
  corepack_executable="$(command -v corepack)"
  corepack_source="$("$package_manager_node" --input-type=module - "$corepack_executable" <<'EOF'
import fs from "node:fs";
process.stdout.write(fs.realpathSync(process.argv[2]));
EOF
  )"
  corepack_source_directory="$(dirname "$corepack_source")"
  corepack_package_root="$(dirname "$corepack_source_directory")"
  corepack_relative_entry="${corepack_source#"$corepack_package_root"/}"

  package_manager_root="$temporary/package-manager"
  package_manager_home="$package_manager_root/corepack-home"
  package_manager_bin="$package_manager_root/bin"
  package_manager_archive="$package_manager_root/corepack.tgz"
  package_manager_log="$package_manager_root/materialize.log"
  mkdir -p "$package_manager_root" "$package_manager_home" "$package_manager_bin"
  cp -R "$corepack_package_root" "$package_manager_root/corepack-cli"
  package_manager_corepack="$package_manager_root/corepack-cli/$corepack_relative_entry"

  if [[ -n "${COREPACK_HOME:-}" ]]; then
    corepack_homes+=("$COREPACK_HOME")
  fi
  if [[ -n "${XDG_CACHE_HOME:-}" ]]; then
    corepack_homes+=("$XDG_CACHE_HOME/node/corepack")
  fi
  corepack_homes+=("$HOME/.cache/node/corepack")

  for candidate in "${corepack_homes[@]}"; do
    if [[ ! -d "$candidate" ]]; then
      continue
    fi
    rm -f "$package_manager_archive"
    if (
      cd "$isolated_repository"
      COREPACK_ENABLE_NETWORK=0 \
        COREPACK_HOME="$candidate" \
        NODE_DISABLE_COMPILE_CACHE=1 \
        "$package_manager_node" "$package_manager_corepack" \
          pack "$package_manager_spec" --output "$package_manager_archive"
    ) >"$package_manager_log" 2>&1; then
      break
    fi
  done
  if [[ ! -f "$package_manager_archive" ]]; then
    printf 'The verified %s Corepack payload is not available offline. Run make bootstrap first.\n' \
      "$package_manager_spec" >&2
    return 1
  fi

  COREPACK_ENABLE_NETWORK=0 \
    COREPACK_HOME="$package_manager_home" \
    NODE_DISABLE_COMPILE_CACHE=1 \
    "$package_manager_node" "$package_manager_corepack" \
      install -g --cache-only "$package_manager_archive" >"$package_manager_log" 2>&1
  rm -f "$package_manager_archive" "$package_manager_log"

  package_version="$("$package_manager_node" --input-type=module - \
    "$package_manager_home/v1/pnpm/$pinned_pnpm_version/package.json" <<'EOF'
import fs from "node:fs";
const manifest = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
process.stdout.write(manifest.version);
EOF
)"
  actual_version="$(cd "$isolated_repository" && run_pinned_pnpm --version)"
  if [[ "$package_version" != "$pinned_pnpm_version" || "$actual_version" != "$pinned_pnpm_version" ]]; then
    printf 'Materialized pnpm version mismatch: expected %s, package %s, executable %s.\n' \
      "$pinned_pnpm_version" "$package_version" "$actual_version" >&2
    return 1
  fi

  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    'package_manager_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)"' \
    'exec env COREPACK_ENABLE_NETWORK=0 COREPACK_HOME="$package_manager_root/corepack-home" NODE_DISABLE_COMPILE_CACHE=1 \' \
    '  node "$package_manager_root/corepack-cli/dist/corepack.js" pnpm "$@"' \
    >"$package_manager_bin/pnpm"
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    'package_manager_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)"' \
    'exec env COREPACK_ENABLE_NETWORK=0 COREPACK_HOME="$package_manager_root/corepack-home" NODE_DISABLE_COMPILE_CACHE=1 \' \
    '  node "$package_manager_root/corepack-cli/dist/corepack.js" "$@"' \
    >"$package_manager_bin/corepack"
  chmod 0555 "$package_manager_bin/pnpm" "$package_manager_bin/corepack"
  chmod -R a-w "$package_manager_root"

  package_manager_baseline="$temporary/package-manager-baseline.manifest"
  write_manifest "$package_manager_root" "$package_manager_baseline"
}

verify_pinned_package_manager() {
  local actual_version package_manager_current phase="$1"
  if [[ -z "$package_manager_root" ]]; then
    return
  fi
  actual_version="$(cd "$isolated_repository" && run_pinned_pnpm --version)"
  if [[ "$actual_version" != "$pinned_pnpm_version" ]]; then
    printf 'Generation %s resolved pnpm %s instead of pinned pnpm %s.\n' \
      "$phase" "$actual_version" "$pinned_pnpm_version" >&2
    return 1
  fi
  package_manager_current="$temporary/package-manager-current.manifest"
  write_manifest "$package_manager_root" "$package_manager_current"
  compare_manifests \
    "$package_manager_baseline" "$package_manager_current" "$phase" package-manager
}

materialize_dependency_snapshot() {
  local source relative relative_parent destination dependency_entry entry_name pnpm_store dependency_workspace
  dependency_workspace="${dependency_root%/*}/dependency-workspace"
  mkdir -p "$dependency_root" "$dependency_workspace"

  if [[ -f "$isolated_repository/pnpm-lock.yaml" && -f "$isolated_repository/pnpm-workspace.yaml" ]]; then
    # Install in a separate staging tree so dependencies never occupy the
    # repository that runs generation. Copy imports prevent store hardlinks.
    git -C "$isolated_repository" archive --format=tar HEAD | tar -xf - -C "$dependency_workspace"
    pnpm_store="$(cd "$dependency_workspace" && run_pinned_pnpm store path)"
    (
      cd "$dependency_workspace"
      CI=true run_pinned_pnpm install \
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
  local -a fixture_environment
  before="$(mktemp "$temporary/fixture-before.XXXXXX")"
  after="$(mktemp "$temporary/fixture-after.XXXXXX")"
  gate_temporary="$(mktemp -d "$temporary/fixture-gate.XXXXXX")"
  worktrees_before="$(git -C "$fixture" worktree list --porcelain)"
  write_manifest "$fixture" "$before" true

  fixture_environment=("TMPDIR=$gate_temporary")
  if [[ -n "${GENERATION_GATE_SELF_TEST_CALLER_HOME:-}" ]]; then
    fixture_environment+=(
      "HOME=$GENERATION_GATE_SELF_TEST_CALLER_HOME"
      "XDG_CACHE_HOME=$GENERATION_GATE_SELF_TEST_CALLER_XDG_CACHE"
      "COREPACK_HOME=$GENERATION_GATE_SELF_TEST_BOOTSTRAP_COREPACK_HOME"
      "PATH=$GENERATION_GATE_SELF_TEST_CALLER_PATH"
      "PNPM=$GENERATION_GATE_SELF_TEST_GLOBAL_PNPM"
      "COREPACK_ENABLE_NETWORK=1"
      "npm_config_offline=false"
      "pnpm_config_offline=false"
    )
  fi
  if fixture_output="$(cd "$fixture" && \
    env "${fixture_environment[@]}" ./tools/check-generated.sh bash -c "$command" 2>&1)"; then
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
  local fixture path pass_counter package_fixture package_manager_check
  local bootstrap_corepack_home candidate record_lines unique_homes unique_xdg_caches
  local poison_home poison_bin poison_marker poison_store environment_record mutation_pass
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

  package_fixture="$temporary/package-manager-repository"
  mkdir -p "$package_fixture/tools"
  cp "${BASH_SOURCE[0]}" "$package_fixture/tools/check-generated.sh"
  chmod +x "$package_fixture/tools/check-generated.sh"
  while IFS= read -r path; do
    mkdir -p "$package_fixture/$(dirname "$path")"
    printf 'generated baseline\n' >"$package_fixture/$path"
  done < <(expected_generated_paths)
  printf 'generated files only\n' >"$package_fixture/gen/README.md"
  printf 'node_modules/\n' >"$package_fixture/.gitignore"
  printf '%s\n' \
    '{"name":"generation-gate-package-manager-self-test","private":true,"packageManager":"pnpm@11.12.0"}' \
    >"$package_fixture/package.json"
  printf '%s\n' 'packages: []' >"$package_fixture/pnpm-workspace.yaml"
  printf '%s\n' \
    "lockfileVersion: '9.0'" \
    '' \
    'settings:' \
    '  autoInstallPeers: true' \
    '  excludeLinksFromLockfile: false' \
    '' \
    'importers:' \
    '  .: {}' \
    >"$package_fixture/pnpm-lock.yaml"
  git -C "$package_fixture" init -q
  git -C "$package_fixture" config user.email 'generation-gate-self-test@layerdraw.invalid'
  git -C "$package_fixture" config user.name 'LayerDraw generation gate self-test'
  git -C "$package_fixture" add .
  git -C "$package_fixture" commit -qm 'test: initialize package-manager fixture'

  bootstrap_corepack_home=''
  for candidate in \
    "${COREPACK_HOME:-}" \
    "${XDG_CACHE_HOME:+$XDG_CACHE_HOME/node/corepack}" \
    "$HOME/.cache/node/corepack"; do
    if [[ -n "$candidate" && -f "$candidate/v1/pnpm/$pinned_pnpm_version/.corepack" ]]; then
      bootstrap_corepack_home="$candidate"
      break
    fi
  done
  if [[ -z "$bootstrap_corepack_home" ]]; then
    printf 'Generation gate self-test requires the verified pnpm %s Corepack payload; run make bootstrap.\n' \
      "$pinned_pnpm_version" >&2
    return 1
  fi

  poison_home="$temporary/caller-home"
  poison_bin="$temporary/caller-bin"
  poison_marker="$temporary/global-pnpm-used"
  environment_record="$temporary/package-manager-environments"
  mkdir -p "$poison_home" "$poison_bin" "$poison_home/xdg-cache"
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    ': >"${GENERATION_GATE_SELF_TEST_GLOBAL_PNPM_MARKER:?}"' \
    'printf "99.99.99\n"' \
    >"$poison_bin/pnpm"
  chmod +x "$poison_bin/pnpm"
  poison_store="$(cd "$package_fixture" && \
    HOME="$poison_home" \
    XDG_CACHE_HOME="$poison_home/xdg-cache" \
    COREPACK_HOME="$bootstrap_corepack_home" \
    COREPACK_ENABLE_NETWORK=0 \
    corepack pnpm store path)"
  mkdir -p "$temporary/package-manager-bootstrap-tmp"
  (
    cd "$package_fixture"
    TMPDIR="$temporary/package-manager-bootstrap-tmp" \
      HOME="$poison_home" \
      XDG_CACHE_HOME="$poison_home/xdg-cache" \
      COREPACK_HOME="$bootstrap_corepack_home" \
      COREPACK_ENABLE_NETWORK=0 \
      CI=true \
      corepack pnpm install \
        --offline \
        --frozen-lockfile \
        --ignore-scripts \
        --store-dir "$poison_store" >/dev/null
  )

  export GENERATION_GATE_SELF_TEST_CALLER_HOME="$poison_home"
  export GENERATION_GATE_SELF_TEST_CALLER_XDG_CACHE="$poison_home/xdg-cache"
  export GENERATION_GATE_SELF_TEST_BOOTSTRAP_COREPACK_HOME="$bootstrap_corepack_home"
  export GENERATION_GATE_SELF_TEST_CALLER_PATH="$poison_bin:$PATH"
  export GENERATION_GATE_SELF_TEST_GLOBAL_PNPM="$poison_bin/pnpm"
  export GENERATION_GATE_SELF_TEST_GLOBAL_PNPM_MARKER="$poison_marker"
  export GENERATION_GATE_SELF_TEST_ENVIRONMENT_RECORD="$environment_record"

  package_manager_check='
    [[ "${COREPACK_ENABLE_NETWORK:-}" == 0 ]] || exit 31
    [[ "${npm_config_offline:-}" == true ]] || exit 32
    [[ "${pnpm_config_offline:-}" == true ]] || exit 33
    [[ "${PNPM:-}" == pnpm ]] || exit 34
    [[ "$HOME" != "$GENERATION_GATE_SELF_TEST_CALLER_HOME" ]] || exit 35
    [[ "$XDG_CACHE_HOME" != "$GENERATION_GATE_SELF_TEST_CALLER_XDG_CACHE" ]] || exit 36
    [[ "$COREPACK_HOME" != "$GENERATION_GATE_SELF_TEST_BOOTSTRAP_COREPACK_HOME" ]] || exit 37
    [[ "$(command -v pnpm)" == "$GENERATION_GATE_PACKAGE_MANAGER_BIN/pnpm" ]] || exit 38
    [[ "$(command -v corepack)" == "$GENERATION_GATE_PACKAGE_MANAGER_BIN/corepack" ]] || exit 39
    [[ "$(pnpm --version)" == "$GENERATION_GATE_PINNED_PNPM_VERSION" ]] || exit 40
    [[ "$(corepack pnpm --version)" == "$GENERATION_GATE_PINNED_PNPM_VERSION" ]] || exit 41
    [[ ! -w "$COREPACK_HOME/v1/pnpm/$GENERATION_GATE_PINNED_PNPM_VERSION/package.json" ]] || exit 42
    [[ ! -e "$GENERATION_GATE_SELF_TEST_GLOBAL_PNPM_MARKER" ]] || exit 43
    printf "%s\t%s\t%s\t%s\n" "$GENERATION_GATE_PASS" "$HOME" "$XDG_CACHE_HOME" \
      "$(command -v pnpm)" >>"$GENERATION_GATE_SELF_TEST_ENVIRONMENT_RECORD"
  '
  assert_fixture_accepts "$package_fixture" "$package_manager_check"
  record_lines="$(wc -l <"$environment_record" | tr -d ' ')"
  unique_homes="$(cut -f2 "$environment_record" | LC_ALL=C sort -u | wc -l | tr -d ' ')"
  unique_xdg_caches="$(cut -f3 "$environment_record" | LC_ALL=C sort -u | wc -l | tr -d ' ')"
  if [[ "$record_lines" != 2 || "$unique_homes" != 2 || "$unique_xdg_caches" != 2 ]] ||
    ! diff -u <(printf '1\n2\n') <(cut -f1 "$environment_record") >/dev/null; then
    printf 'Generation gate self-test did not observe two independently cached package-manager passes.\n' >&2
    return 1
  fi
  if [[ -e "$poison_marker" ]]; then
    printf 'Generation gate self-test invoked the caller global pnpm.\n' >&2
    return 1
  fi

  for mutation_pass in 1 2; do
    export GENERATION_GATE_SELF_TEST_MUTATION_PASS="$mutation_pass"
    assert_fixture_rejects "$package_fixture" \
      'if [[ "$GENERATION_GATE_PASS" == "$GENERATION_GATE_SELF_TEST_MUTATION_PASS" ]]; then target="$COREPACK_HOME/v1/pnpm/$GENERATION_GATE_PINNED_PNPM_VERSION/package.json"; chmod u+w "$target"; printf "\n" >>"$target"; fi' \
      'corepack-home/v1/pnpm/11.12.0/package.json'
    if ! grep -Fq "Generation pass $mutation_pass changed the preserved package-manager bundle" <<<"$fixture_output"; then
      printf '%s\n' "$fixture_output" >&2
      printf 'Generation gate self-test did not identify pass-%s package-manager cache mutation.\n' \
        "$mutation_pass" >&2
      return 1
    fi
  done
  unset GENERATION_GATE_SELF_TEST_MUTATION_PASS

  unset GENERATION_GATE_SELF_TEST_CALLER_HOME \
    GENERATION_GATE_SELF_TEST_CALLER_XDG_CACHE \
    GENERATION_GATE_SELF_TEST_BOOTSTRAP_COREPACK_HOME \
    GENERATION_GATE_SELF_TEST_CALLER_PATH \
    GENERATION_GATE_SELF_TEST_GLOBAL_PNPM \
    GENERATION_GATE_SELF_TEST_GLOBAL_PNPM_MARKER \
    GENERATION_GATE_SELF_TEST_ENVIRONMENT_RECORD

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
caller_manifest="$temporary/caller-before.manifest"
write_manifest "$repository_root" "$caller_manifest" true

revision="$(git -C "$repository_root" rev-parse HEAD)"
source_date_epoch="$(git -C "$repository_root" show -s --format=%ct "$revision")"
go_mod_cache="$(go env GOMODCACHE)"
for pass in 1 2; do
  pass_root="$temporary/pass-$pass"
  isolated_repository="$pass_root/repository"
  dependency_root="$pass_root/dependencies"
  repository_baseline="$pass_root/repository-baseline.manifest"
  dependency_baseline="$pass_root/dependency-baseline.manifest"
  repository_current="$pass_root/repository-current.manifest"
  dependency_current="$pass_root/dependency-current.manifest"
  pass_cache="$pass_root/cache"
  mkdir -p "$pass_root" \
    "$pass_cache/home" \
    "$pass_cache/xdg-cache" \
    "$pass_cache/xdg-config" \
    "$pass_cache/xdg-data" \
    "$pass_cache/xdg-state" \
    "$pass_cache/turbo" \
    "$pass_cache/go-build" \
    "$pass_cache/runtime-tmp"

  git clone --quiet --no-local --no-hardlinks --no-checkout \
    "$repository_root" "$isolated_repository"
  git -C "$isolated_repository" checkout --quiet --detach "$revision"
  git -C "$isolated_repository" remote remove origin
  if (( pass == 1 )); then
    materialize_package_manager
  fi
  materialize_dependency_snapshot

  cd "$isolated_repository"
  assert_expected_paths
  verify_pinned_package_manager "pass $pass before generation"
  write_manifest "$isolated_repository" "$repository_baseline" true
  write_manifest "$dependency_root" "$dependency_baseline"

  generation_environment=(
    "TMPDIR=$pass_cache/runtime-tmp"
    "HOME=$pass_cache/home"
    "XDG_CACHE_HOME=$pass_cache/xdg-cache"
    "XDG_CONFIG_HOME=$pass_cache/xdg-config"
    "XDG_DATA_HOME=$pass_cache/xdg-data"
    "XDG_STATE_HOME=$pass_cache/xdg-state"
    "LC_ALL=C"
    "TZ=UTC"
    "SOURCE_DATE_EPOCH=$source_date_epoch"
    "COREPACK_ENABLE_NETWORK=0"
    "npm_config_offline=true"
    "pnpm_config_offline=true"
    "TURBO_CACHE_DIR=$pass_cache/turbo"
    "TURBO_TELEMETRY_DISABLED=1"
    "NODE_DISABLE_COMPILE_CACHE=1"
    "GOCACHE=$pass_cache/go-build"
    "GOTMPDIR=$pass_cache/runtime-tmp"
    "GOMODCACHE=$go_mod_cache"
    "GOPROXY=off"
    "npm_config_store_dir=$pass_cache/pnpm-store"
    "pnpm_config_verify_deps_before_run=false"
    "GENERATION_GATE_PASS=$pass"
  )
  if [[ -n "$package_manager_root" ]]; then
    generation_environment+=(
      "COREPACK_HOME=$package_manager_home"
      "PATH=$package_manager_bin:$PATH"
      "PNPM=pnpm"
      "GENERATION_GATE_PACKAGE_MANAGER_BIN=$package_manager_bin"
      "GENERATION_GATE_PINNED_PNPM_VERSION=$pinned_pnpm_version"
    )
  fi

  # The frozen install above is the dependency check. pnpm's default pre-exec
  # check rewrites link metadata, so disable that redundant mutating step.
  env "${generation_environment[@]}" "$@"
  assert_expected_paths
  write_manifest "$isolated_repository" "$repository_current" true
  write_manifest "$dependency_root" "$dependency_current"
  compare_manifests "$repository_baseline" "$repository_current" "pass $pass" repository
  compare_manifests "$dependency_baseline" "$dependency_current" "pass $pass" dependencies
  verify_pinned_package_manager "pass $pass"
done
