#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

platform="${1:?usage: smoke-desktop-installer.sh PLATFORM INSTALLER}"
installer="${2:?usage: smoke-desktop-installer.sh PLATFORM INSTALLER}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/layerdraw-desktop-smoke.XXXXXX")"
cleanup() {
  if [[ -n "${mounted_volume:-}" ]]; then hdiutil detach -quiet "$mounted_volume" >/dev/null 2>&1 || true; fi
  chmod -R u+w "$temporary" >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT

export XDG_CONFIG_HOME="$temporary/config"
mkdir -p "$XDG_CONFIG_HOME/layerdraw" "$temporary/user-data"
marker="$temporary/user-data/upgrade-marker"
printf 'preserve-me\n' > "$marker"

case "$platform" in
  darwin)
    mounted_volume="$temporary/volume"
    mkdir -p "$mounted_volume" "$temporary/Applications"
    hdiutil attach -quiet -nobrowse -mountpoint "$mounted_volume" "$installer"
    source_app="$(find "$mounted_volume" -maxdepth 1 -type d -name '*.app' -print -quit)"
    if [[ -z "$source_app" ]]; then printf 'DMG does not contain an application bundle.\n' >&2; exit 1; fi
    installed_app="$temporary/Applications/$(basename "$source_app")"
    cp -R "$source_app" "$temporary/Applications/"
    if [[ "${LAYERDRAW_EXPECT_SIGNED:-0}" == "1" ]]; then
      codesign --verify --deep --strict "$installed_app"
      codesign --verify --strict "$installed_app/Contents/Resources/layerdraw/bin/layerdraw-host"
    fi
    "$installed_app/Contents/MacOS/LayerDraw" --packaged-probe > "$temporary/probe.json"
    test -x "$installed_app/Contents/Resources/layerdraw/bin/layerdraw-host"
    test -f "$installed_app/Contents/Resources/layerdraw/desktop-capabilities.json"
    test -f "$installed_app/Contents/Resources/layerdraw/host/layerdraw-host.cdx.json"
    if find "$installed_app" -type f \( -name '*.map' -o -path '*/testdata/*' -o -path '*/test-fixtures/*' \) -print -quit | read -r; then
      printf 'macOS installer contains development-only assets.\n' >&2; exit 1
    fi
    rm -rf "$installed_app"
    cp -R "$source_app" "$temporary/Applications/"
    test "$(cat "$marker")" = "preserve-me"
    installed_root="$installed_app"
    installed_executable="$installed_app/Contents/MacOS/LayerDraw"
    ;;
  linux)
    install_root="$temporary/install-root"
    dpkg-deb --info "$installer" >/dev/null
    dpkg-deb -x "$installer" "$install_root"
    xvfb-run -a "$install_root/usr/bin/layerdraw" --packaged-probe > "$temporary/probe.json"
    test -x "$install_root/usr/lib/layerdraw/layerdraw-host"
    test -f "$install_root/usr/share/layerdraw/legal/desktop-capabilities.json"
    test -f "$install_root/usr/share/layerdraw/legal/host/layerdraw-host.cdx.json"
    if find "$install_root" -type f \( -name '*.map' -o -path '*/testdata/*' -o -path '*/test-fixtures/*' \) -print -quit | read -r; then
      printf 'Linux installer contains development-only assets.\n' >&2; exit 1
    fi
    dpkg-deb -x "$installer" "$install_root"
    test "$(cat "$marker")" = "preserve-me"
    installed_root="$install_root"
    installed_executable="$install_root/usr/bin/layerdraw"
    ;;
  *) printf 'Unsupported smoke-test platform: %s\n' "$platform" >&2; exit 1 ;;
esac

corrupt="$temporary/corrupt-installer"
head -c 64 "$installer" > "$corrupt"
if [[ "$platform" == "darwin" ]]; then
  if hdiutil attach -quiet -nobrowse "$corrupt" >/dev/null 2>&1; then printf 'Corrupt DMG was accepted.\n' >&2; exit 1; fi
else
  if dpkg-deb --info "$corrupt" >/dev/null 2>&1; then printf 'Corrupt deb was accepted.\n' >&2; exit 1; fi
fi
test -x "$installed_executable"
test "$(cat "$marker")" = "preserve-me"
rm -rf "$installed_root"
test ! -e "$installed_executable"

go run ./tools/desktopprobe -input "$temporary/probe.json" verify
