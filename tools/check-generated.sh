#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

temporary=''
isolated_worktree=''
repository_root=''

cleanup() {
  if [[ -n "$isolated_worktree" && -n "$repository_root" ]]; then
    git -C "$repository_root" worktree remove --force "$isolated_worktree" >/dev/null 2>&1 || true
  fi
  if [[ -n "$temporary" ]]; then
    rm -rf "$temporary"
  fi
}
trap cleanup EXIT

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
    return 1
  fi
  rm -f "$expected" "$actual"
}

changed_paths() {
  {
    git diff --name-only --no-renames HEAD --
    git ls-files --others --exclude-standard
  } | LC_ALL=C sort -u
}

assert_repository_clean() {
  local phase changes unexpected
  phase="$1"
  changes="$(changed_paths)"
  if [[ -z "$changes" ]]; then
    return
  fi

  unexpected="$({
    while IFS= read -r path; do
      if ! expected_generated_paths | grep -Fxq -- "$path"; then
        printf '%s\n' "$path"
      fi
    done <<<"$changes"
  })"
  if [[ -n "$unexpected" ]]; then
    printf '%s\n' "$unexpected" >&2
    printf 'Generation %s changed paths outside the declared exact generated-output set.\n' "$phase" >&2
    return 1
  fi

  printf '%s\n' "$changes" >&2
  printf 'Generated files changed during generation %s. Run make generate and commit the result.\n' "$phase" >&2
  return 1
}

link_dependency_trees() {
  local dependency_path relative_path destination entry
  while IFS= read -r -d '' dependency_path; do
    relative_path="${dependency_path#"$repository_root"/}"
    destination="$isolated_worktree/$relative_path"
    mkdir -p "$destination"
    while IFS= read -r -d '' entry; do
      ln -s "$entry" "$destination/$(basename "$entry")"
    done < <(find "$dependency_path" -mindepth 1 -maxdepth 1 -print0)
  done < <(
    find "$repository_root" \
      -path "$repository_root/.git" -prune -o \
      -name node_modules -prune -print0
  )
}

assert_fixture_preserved() {
  local fixture="$1"
  if [[ "$(<"$fixture/caller.txt")" != 'caller change' ]]; then
    printf 'Generation gate self-test disturbed a pre-existing tracked caller change.\n' >&2
    return 1
  fi
  if [[ "$(<"$fixture/caller-untracked.txt")" != 'caller untracked' ]]; then
    printf 'Generation gate self-test disturbed a pre-existing untracked caller file.\n' >&2
    return 1
  fi
}

assert_fixture_rejects() {
  local fixture="$1"
  local command="$2"
  local expected_path="$3"
  local output
  if output="$(cd "$fixture" && ./tools/check-generated.sh bash -c "$command" 2>&1)"; then
    printf 'Generation gate self-test accepted an out-of-allowlist change to %s.\n' "$expected_path" >&2
    return 1
  fi
  if ! grep -Fxq -- "$expected_path" <<<"$output"; then
    printf '%s\n' "$output" >&2
    printf 'Generation gate self-test did not report %s.\n' "$expected_path" >&2
    return 1
  fi
  assert_fixture_preserved "$fixture"
}

run_self_test() {
  local fixture path
  repository_root=''
  temporary="$(mktemp -d)"
  fixture="$temporary/repository"
  mkdir -p "$fixture/tools"
  cp "${BASH_SOURCE[0]}" "$fixture/tools/check-generated.sh"
  chmod +x "$fixture/tools/check-generated.sh"

  while IFS= read -r path; do
    mkdir -p "$fixture/$(dirname "$path")"
    printf 'generated baseline\n' >"$fixture/$path"
  done < <(expected_generated_paths)
  printf 'tracked baseline\n' >"$fixture/tracked-outside.txt"
  printf 'caller baseline\n' >"$fixture/caller.txt"

  git -C "$fixture" init -q
  git -C "$fixture" config user.email 'generation-gate-self-test@layerdraw.invalid'
  git -C "$fixture" config user.name 'LayerDraw generation gate self-test'
  git -C "$fixture" add .
  git -C "$fixture" commit -qm 'test: initialize generation gate fixture'

  printf 'caller change\n' >"$fixture/caller.txt"
  printf 'caller untracked\n' >"$fixture/caller-untracked.txt"

  (cd "$fixture" && ./tools/check-generated.sh bash -c ':')
  assert_fixture_preserved "$fixture"
  assert_fixture_rejects "$fixture" 'touch unexpected-output.txt' 'unexpected-output.txt'
  assert_fixture_rejects "$fixture" ': > tracked-outside.txt' 'tracked-outside.txt'
  assert_fixture_rejects "$fixture" 'rm tracked-outside.txt' 'tracked-outside.txt'

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
temporary="$(mktemp -d)"
isolated_worktree="$temporary/worktree"
git -C "$repository_root" worktree add --quiet --detach "$isolated_worktree" HEAD
link_dependency_trees

cd "$isolated_worktree"
assert_expected_paths
assert_repository_clean 'baseline'
"$@"
assert_expected_paths
assert_repository_clean 'pass 1'
"$@"
assert_expected_paths
assert_repository_clean 'pass 2'
