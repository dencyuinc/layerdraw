#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="0.17.0"
platform="$(uname -s)"
machine="$(uname -m)"

case "$platform/$machine" in
  Darwin/arm64)
    asset="liblbug-static-osx-arm64.tar.gz"
    digest="5a0323b4615222f71c2f20ec3ee2c7718409449138668b98bcf587a8d6b65444"
    target_platform="darwin-arm64"
    extension_platform="osx_arm64"
    extension_digest="b7ab6ba81e48f0350f91836a13348ee0f2fb493c824604ea2ecedaf7fb62ac80"
    ;;
  Darwin/x86_64)
    asset="liblbug-static-osx-x86_64.tar.gz"
    digest="08251d2865deb299e5e9db06447bb86543c91f1b1042d4401a4bf46731a76694"
    target_platform="darwin-amd64"
    extension_platform="osx_amd64"
    extension_digest="6d3501013330a0bc89c47f49d3ee9e1dd7d483fa4ac31c7c1a5e615425358a7c"
    ;;
  Linux/x86_64)
    asset="liblbug-static-linux-x86_64-compat.tar.gz"
    digest="d21ae0963a2b1256882954b68e4fca5d16a28a6b27201e4d06f58775f4b311b9"
    target_platform="linux-amd64"
    extension_platform="linux_amd64"
    extension_digest="327230badf037615f21fc52c347f0cb9d4b55b62a7e295bdd577379aacd842f4"
    ;;
  Linux/aarch64|Linux/arm64)
    asset="liblbug-static-linux-aarch64-compat.tar.gz"
    digest="a5b7d780bbc1cb312de5907ee54a2d509d4f42a5712565c06431c9f9a2a260d6"
    target_platform="linux-arm64"
    extension_platform="linux_arm64"
    extension_digest="5faab293191ebdf69f9152d724a5731d3cf83b781688f95106b8e4b64b222a8f"
    ;;
  *)
    printf 'Unsupported Ladybug native platform: %s/%s\n' "$platform" "$machine" >&2
    exit 1
    ;;
esac

cache_root="${LAYERDRAW_LADYBUG_CACHE:-$repository_root/.cache/ladybug-native}"
download_root="$cache_root/downloads"
install_root="$cache_root/v$version/$target_platform"
archive="$download_root/$asset"
url="https://github.com/LadybugDB/ladybug/releases/download/v$version/$asset"
extension_asset="libfts-$extension_platform-v$version.lbug_extension"
extension_archive="$download_root/$extension_asset"
extension_url="https://extension.ladybugdb.com/v$version/$extension_platform/fts/libfts.lbug_extension"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

mkdir -p "$download_root" "$(dirname "$install_root")"
if [[ ! -f "$archive" || "$(sha256_file "$archive")" != "$digest" ]]; then
  temporary_archive="$(mktemp "$download_root/.ladybug-download.XXXXXX")"
  trap 'rm -f "$temporary_archive"' EXIT
  curl --fail --location --retry 3 --retry-all-errors --output "$temporary_archive" "$url" >&2
  actual_digest="$(sha256_file "$temporary_archive")"
  if [[ "$actual_digest" != "$digest" ]]; then
    printf 'Ladybug archive digest mismatch: expected %s, got %s\n' "$digest" "$actual_digest" >&2
    exit 1
  fi
  mv "$temporary_archive" "$archive"
  trap - EXIT
fi

if [[ ! -f "$extension_archive" || "$(sha256_file "$extension_archive")" != "$extension_digest" ]]; then
  temporary_extension="$(mktemp "$download_root/.ladybug-fts-download.XXXXXX")"
  trap 'rm -f "$temporary_extension"' EXIT
  curl --fail --location --retry 3 --retry-all-errors --output "$temporary_extension" "$extension_url" >&2
  actual_digest="$(sha256_file "$temporary_extension")"
  if [[ "$actual_digest" != "$extension_digest" ]]; then
    printf 'Ladybug FTS extension digest mismatch: expected %s, got %s\n' "$extension_digest" "$actual_digest" >&2
    exit 1
  fi
  mv "$temporary_extension" "$extension_archive"
  trap - EXIT
fi

stage="$(mktemp -d "$(dirname "$install_root")/.ladybug-install.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
tar -xzf "$archive" -C "$stage"
for required in lbug.h lbug.hpp liblbug.a; do
  if [[ ! -f "$stage/$required" ]]; then
    printf 'Ladybug archive is missing %s\n' "$required" >&2
    exit 1
  fi
done
cp "$extension_archive" "$stage/libfts.lbug_extension"
unexpected="$(find "$stage" -mindepth 1 -maxdepth 1 -type f ! -name lbug.h ! -name lbug.hpp ! -name liblbug.a ! -name libfts.lbug_extension -print -quit)"
if [[ -n "$unexpected" ]]; then
  printf 'Ladybug archive contains unexpected file: %s\n' "$unexpected" >&2
  exit 1
fi
rm -rf "$install_root"
mv "$stage" "$install_root"
trap - EXIT

printf '%s\n' "$install_root"
