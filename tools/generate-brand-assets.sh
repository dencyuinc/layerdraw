#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
brand_dir="${repo_root}/brand"
png_dir="${brand_dir}/png"
icon_source="${brand_dir}/layerdraw-icon.svg"
light_logo_source="${brand_dir}/layerdraw-logo-on-light.svg"
dark_logo_source="${brand_dir}/layerdraw-logo-on-dark.svg"

for command_name in rsvg-convert magick; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    printf 'error: required command not found: %s\n' "${command_name}" >&2
    exit 1
  fi
done

mkdir -p "${png_dir}"

for size in 16 32 48 128 180 192 256 512 1024; do
  rsvg-convert \
    --width "${size}" \
    --height "${size}" \
    --output "${png_dir}/layerdraw-icon-${size}.png" \
    "${icon_source}"
done

rsvg-convert \
  --width 1200 \
  --output "${png_dir}/layerdraw-logo-on-light-1200.png" \
  "${light_logo_source}"

rsvg-convert \
  --width 1200 \
  --output "${png_dir}/layerdraw-logo-on-dark-1200.png" \
  "${dark_logo_source}"

magick \
  "${png_dir}/layerdraw-icon-16.png" \
  "${png_dir}/layerdraw-icon-32.png" \
  "${png_dir}/layerdraw-icon-48.png" \
  "${brand_dir}/favicon.ico"

social_logo="$(mktemp "${TMPDIR:-/tmp}/layerdraw-social-logo.XXXXXX.png")"
trap 'rm -f "${social_logo}"' EXIT

rsvg-convert \
  --width 900 \
  --output "${social_logo}" \
  "${dark_logo_source}"

render_social_image() {
  local dimensions="$1"
  local output="$2"

  magick \
    -size "${dimensions}" \
    'canvas:#0d1117' \
    "${social_logo}" \
    -gravity center \
    -composite \
    -strip \
    -depth 8 \
    "${output}"
}

render_social_image 1280x640 "${brand_dir}/github-social-preview.png"
render_social_image 1200x630 "${brand_dir}/og-image.png"
