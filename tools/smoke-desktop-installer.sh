#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

platform="${1:?usage: smoke-desktop-installer.sh PLATFORM PREVIOUS_INSTALLER [CURRENT_INSTALLER]}"
previous_installer="${2:?usage: smoke-desktop-installer.sh PLATFORM PREVIOUS_INSTALLER [CURRENT_INSTALLER]}"
current_installer="${3:-$previous_installer}"
temporary_base="${TMPDIR:-/tmp}"
temporary_base="${temporary_base%/}"
temporary="$(mktemp -d "$temporary_base/layerdraw-desktop-smoke.XXXXXX")"
probe_state_key="$(openssl rand -hex 16)"
probe_state_root="$temporary_base/layerdraw-desktop-probe-state-$probe_state_key"
cleanup() {
  if [[ -n "${mounted_volume:-}" ]]; then hdiutil detach -quiet "$mounted_volume" >/dev/null 2>&1 || true; fi
  chmod -R u+w "$temporary" >/dev/null 2>&1 || true
  rm -rf "$temporary"
  rm -rf "$probe_state_root"
}
trap cleanup EXIT

export XDG_CONFIG_HOME="$temporary/config"
export LAYERDRAW_DESKTOP_PROBE_STATE_KEY="$probe_state_key"
mkdir -p "$XDG_CONFIG_HOME"

verify_probe() {
  local executable="$1"
  local action="$2"
  LAYERDRAW_DESKTOP_PROBE_ACTION="$action" "$executable" --packaged-probe > "$temporary/probe.json"
  go run ./tools/desktopprobe -input "$temporary/probe.json" verify
}

verify_payload() {
  local root="$1"
  local host="$2"
  local capabilities="$3"
  local host_sbom="$4"
  local conformance="$5"
  test -x "$host"
  test -f "$capabilities"
  test -f "$host_sbom"
  test -f "$conformance"
  if find "$root" -type f \( -name '*.map' -o -path '*/testdata/*' -o -path '*/test-fixtures/*' \) -print -quit | read -r; then
    printf '%s installer contains development-only assets.\n' "$platform" >&2
    exit 1
  fi
}

case "$platform" in
  darwin)
    applications="$temporary/Applications"
    mkdir -p "$applications"
    install_dmg() {
      local installer="$1"
      mounted_volume="$temporary/volume-$(basename "$installer")"
      mkdir -p "$mounted_volume"
      hdiutil attach -quiet -nobrowse -mountpoint "$mounted_volume" "$installer"
      local source_app
      source_app="$(find "$mounted_volume" -maxdepth 1 -type d -name '*.app' -print -quit)"
      if [[ -z "$source_app" ]]; then printf 'DMG does not contain an application bundle.\n' >&2; exit 1; fi
      installed_app="$applications/$(basename "$source_app")"
      rm -rf "$installed_app"
      cp -R "$source_app" "$applications/"
      hdiutil detach -quiet "$mounted_volume"
      mounted_volume=""
    }

    install_dmg "$previous_installer"
    installed_executable="$installed_app/Contents/MacOS/LayerDraw"
    verify_payload "$installed_app" "$installed_app/Contents/Resources/layerdraw/bin/layerdraw-host" "$installed_app/Contents/Resources/layerdraw/desktop-capabilities.json" "$installed_app/Contents/Resources/layerdraw/host/layerdraw-host.cdx.json" "$installed_app/Contents/Resources/layerdraw/desktop-conformance.json"
    verify_probe "$installed_executable" initialize

    corrupt="$temporary/corrupt.dmg"
    head -c 64 "$current_installer" > "$corrupt"
    if hdiutil attach -quiet -nobrowse "$corrupt" >/dev/null 2>&1; then printf 'Corrupt DMG was accepted.\n' >&2; exit 1; fi
    test -x "$installed_executable"
    verify_probe "$installed_executable" verify

    install_dmg "$current_installer"
    installed_executable="$installed_app/Contents/MacOS/LayerDraw"
    verify_payload "$installed_app" "$installed_app/Contents/Resources/layerdraw/bin/layerdraw-host" "$installed_app/Contents/Resources/layerdraw/desktop-capabilities.json" "$installed_app/Contents/Resources/layerdraw/host/layerdraw-host.cdx.json" "$installed_app/Contents/Resources/layerdraw/desktop-conformance.json"
    verify_probe "$installed_executable" verify
    if [[ "${LAYERDRAW_EXPECT_SIGNED:-0}" == "1" ]]; then
      codesign --verify --strict "$current_installer"
      codesign --verify --deep --strict "$installed_app"
      codesign --verify --strict "$installed_app/Contents/Resources/layerdraw/bin/layerdraw-host"
    fi
    if [[ -n "${LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT:-}" ]]; then
      "$installed_executable" --packaged-conformance "$LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT"
    fi
    rm -rf "$installed_app"
    test ! -e "$installed_executable"
    ;;
  linux)
    if dpkg-query -W -f='${Status}' layerdraw 2>/dev/null | grep -q 'install ok installed'; then
      printf 'Refusing to replace an existing system LayerDraw installation.\n' >&2
      exit 1
    fi
    sudo dpkg -i "$previous_installer"
    installed_executable="/usr/bin/layerdraw"
    verify_payload "/usr/share/layerdraw" "/usr/lib/layerdraw/layerdraw-host" "/usr/share/layerdraw/legal/desktop-capabilities.json" "/usr/share/layerdraw/legal/host/layerdraw-host.cdx.json" "/usr/share/layerdraw/legal/desktop-conformance.json"
    verify_probe "$installed_executable" initialize

    corrupt="$temporary/corrupt.deb"
    head -c 64 "$current_installer" > "$corrupt"
    if sudo dpkg -i "$corrupt" >/dev/null 2>&1; then printf 'Corrupt deb was accepted.\n' >&2; exit 1; fi
    test -x "$installed_executable"
    verify_probe "$installed_executable" verify

    sudo dpkg -i "$current_installer"
    verify_payload "/usr/share/layerdraw" "/usr/lib/layerdraw/layerdraw-host" "/usr/share/layerdraw/legal/desktop-capabilities.json" "/usr/share/layerdraw/legal/host/layerdraw-host.cdx.json" "/usr/share/layerdraw/legal/desktop-conformance.json"
    verify_probe "$installed_executable" verify
    if [[ "${LAYERDRAW_EXPECT_SIGNED:-0}" == "1" ]]; then
      gpg --verify "$current_installer.asc" "$current_installer"
    fi
    if [[ -n "${LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT:-}" ]]; then
      "$installed_executable" --packaged-conformance "$LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT"
    fi
    sudo dpkg -r layerdraw
    test ! -e "$installed_executable"
    ;;
  *) printf 'Unsupported smoke-test platform: %s\n' "$platform" >&2; exit 1 ;;
esac

test -f "$probe_state_root/settings-v1.json"
test -f "$probe_state_root/projects/upgrade-probe/document.ldl"
