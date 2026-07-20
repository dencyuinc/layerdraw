#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

mode="${1:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"
platform="${2:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"
format="${3:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"
version="${4:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"
revision="${5:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"
artifacts="${6:?usage: build-desktop-update-metadata.sh MODE PLATFORM FORMAT VERSION REVISION ARTIFACT_DIR}"

case "$format" in
  dmg) suffix="dmg" ;;
  nsis) suffix="exe" ;;
  deb) suffix="deb" ;;
  *) printf 'Unsupported Desktop installer format: %s\n' "$format" >&2; exit 1 ;;
esac

installer="$artifacts/LayerDraw-$version.$suffix"
manifest="$artifacts/LayerDraw-$version-$platform-update.json"
build_args=()
verify_args=()
case "$mode" in
  test)
    build_args=(-test-signing)
    verify_args=(-allow-test-signing)
    ;;
  release)
    : "${UPDATE_SIGNING_KEY:?release metadata requires UPDATE_SIGNING_KEY}"
    : "${UPDATE_PUBLIC_KEY:?release metadata requires UPDATE_PUBLIC_KEY}"
    build_args=(-signing-key-env UPDATE_SIGNING_KEY)
    verify_args=(-trusted-public-key "$UPDATE_PUBLIC_KEY")
    if [[ "$platform" == "linux" ]]; then
      build_args+=(-platform-signature "$installer.asc")
    fi
    ;;
  *) printf 'Unsupported update signing mode: %s\n' "$mode" >&2; exit 1 ;;
esac

commit="$(git rev-parse "$revision^{commit}")"
built_at="$(git log -1 --format=%cI "$commit")"
go run ./tools/desktoprelease build \
  -installer "$installer" \
  -sbom "$artifacts/LayerDraw-$version.cdx.json" \
  -licenses "$artifacts/LayerDraw-$version-THIRD_PARTY_NOTICES.txt" \
  -capabilities "$artifacts/LayerDraw-$version-capabilities.json" \
  -output "$manifest" -version "$version" -minimum-supported-version 0.0.0 \
  -platform "$platform" -format "$format" -source-revision "$commit" -built-at "$built_at" \
  "${build_args[@]}"

go run ./tools/desktoprelease verify \
  -manifest "$manifest" -root "$artifacts" -platform "$platform" \
  -current-version 0.0.0 "${verify_args[@]}"
