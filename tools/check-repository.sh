#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

failed=0

branch_name="${LAYERDRAW_BRANCH_NAME:-}"
event_name="${LAYERDRAW_EVENT_NAME:-}"
pr_author="${LAYERDRAW_PR_AUTHOR:-}"
pr_body="${LAYERDRAW_PR_BODY:-}"
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
  CODE_OF_CONDUCT.md
  SUPPORT.md
  OWNERS.yaml
  .github/CODEOWNERS
  .github/PULL_REQUEST_TEMPLATE.md
  docs/README.md
  docs/legal/README.md
  docs/legal/contributor-license-agreement.md
  docs/legal/contributor-privacy-notice.md
  docs/legal/trademarks.md
  .nvmrc
  .node-version
)

for path in "${required_files[@]}"; do
  if [[ ! -f "$path" ]]; then
    fail "required public repository file is missing: $path"
  fi
done

if [[ -f .nvmrc && -f .node-version ]] && ! cmp -s .nvmrc .node-version; then
  fail '.nvmrc and .node-version must pin the same Node.js version'
fi

nested_go_modules="$({
  find . \
    -path './.git' -prune -o \
    -path './node_modules' -prune -o \
    -mindepth 2 -name go.mod -print
})"

if [[ -n "$nested_go_modules" ]]; then
  printf '%s\n' "$nested_go_modules" >&2
  fail 'nested Go modules are forbidden; use the repository root module'
fi

if [[ -f go.mod ]] && ! grep -Fxq 'module github.com/dencyuinc/layerdraw' go.mod; then
  fail 'root go.mod must declare module github.com/dencyuinc/layerdraw'
fi

default_branch_event=false
if [[ "$branch_name" == "main" && ( "$event_name" == "push" || "$event_name" == "workflow_dispatch" ) ]]; then
  default_branch_event=true
fi

if [[ -n "$branch_name" && "$default_branch_event" != true && ! "$branch_name" =~ $branch_name_pattern && ! "$branch_name" =~ ^dependabot/ ]]; then
  fail "branch name '$branch_name' must use an approved prefix and a lowercase kebab-case description"
fi

if [[ "$event_name" == "pull_request" && "$pr_author" != 'dependabot[bot]' ]]; then
  cla_statement='I have read and agree to the LayerDraw Contributor License Agreement 1.0 and confirm that I have the rights and any employer authorization required to submit this Contribution.'
  if ! grep -Fq -- "- [x] $cla_statement" <<< "$pr_body" && ! grep -Fq -- "- [X] $cla_statement" <<< "$pr_body"; then
    fail 'pull request author must accept the LayerDraw Contributor License Agreement 1.0'
  fi
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

if grep -Eiq 'pre-release draft|承認前draft' docs/legal/contributor-license-agreement.md docs/legal/trademarks.md; then
  fail 'normative contributor or trademark policy is still marked as a pre-release draft'
fi

if (( failed != 0 )); then
  exit 1
fi

printf 'Repository policy checks passed.\n'
