#!/usr/bin/env bash

set -euo pipefail

failed=0

branch_name="${LAYERDRAW_BRANCH_NAME:-}"
branch_name_pattern='^(feat|fix|docs|refactor|test|build|ci|chore|perf|security|revert|release)/[a-z0-9]+(-[a-z0-9]+)*$'

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  failed=1
}

required_files=(
  README.md
  LICENSE
  NOTICE
  CONTRIBUTING.md
  SECURITY.md
  OWNERS.yaml
  .github/CODEOWNERS
  .github/PULL_REQUEST_TEMPLATE.md
  docs/README.md
  docs/legal/README.md
)

for path in "${required_files[@]}"; do
  if [[ ! -f "$path" ]]; then
    fail "required public repository file is missing: $path"
  fi
done

if [[ -n "$branch_name" && "$branch_name" != "main" && ! "$branch_name" =~ $branch_name_pattern && ! "$branch_name" =~ ^dependabot/ ]]; then
  fail "branch name '$branch_name' must use an approved prefix and a lowercase kebab-case description"
fi

forbidden_paths="$({
  git ls-files | grep -E '(^|/)(\.DS_Store|\.env|\.codex|\.idea|node_modules)(/|$)|(^|/)\.env\.(local|development|production|test)$|\.(pem|key)$' || true
})"

if [[ -n "$forbidden_paths" ]]; then
  printf '%s\n' "$forbidden_paths" >&2
  fail 'tracked local, secret-bearing, or generated paths were found'
fi

check_forbidden_text() {
  local pattern="$1"
  local description="$2"
  local matches

  matches="$(git grep -n -I -E -- "$pattern" -- ':!tools/check-repository.sh' || true)"
  if [[ -n "$matches" ]]; then
    printf '%s\n' "$matches" >&2
    fail "$description"
  fi
}

check_forbidden_text '(^|[^[:alnum:]_])language[[:space:]]+1([^[:digit:].]|$)' 'legacy LDL source language header was found'
check_forbidden_text '(^|[^[:alnum:]_])ldl[[:space:]]+1([^[:digit:].]|$)' 'legacy LDL version header was found'
check_forbidden_text 'language_version' 'removed LDL source language_version field was found'
check_forbidden_text '@layerdraw/core' 'removed legacy TypeScript core package name was found'
check_forbidden_text '@gmail\.com' 'personal Gmail address was found in tracked content'
check_forbidden_text 'layerdraw-private-history|/private/layerdraw' 'private-history repository reference was found'

if (( failed != 0 )); then
  exit 1
fi

printf 'Repository policy checks passed.\n'
