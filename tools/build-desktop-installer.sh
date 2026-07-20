#!/usr/bin/env bash

# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
platform="${1:?usage: build-desktop-installer.sh PLATFORM VERSION OUTPUT_DIR}"
version="${2:?usage: build-desktop-installer.sh PLATFORM VERSION OUTPUT_DIR}"
output="${3:?usage: build-desktop-installer.sh PLATFORM VERSION OUTPUT_DIR}"
wails_version="v2.12.0"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf 'Desktop installer version must be numeric semver: %s\n' "$version" >&2
  exit 1
fi

case "$platform" in
  darwin|windows|linux) ;;
  *) printf 'Unsupported Desktop platform: %s\n' "$platform" >&2; exit 1 ;;
esac

if [[ "$platform" == "windows" ]] && ! command -v makensis >/dev/null 2>&1; then
  program_files_x86="$(printenv 'ProgramFiles(x86)' || true)"
  if [[ -n "$program_files_x86" ]] && command -v cygpath >/dev/null 2>&1; then
    nsis_bin="$(cygpath -u "$program_files_x86/NSIS")"
    if [[ -x "$nsis_bin/makensis.exe" ]]; then export PATH="$nsis_bin:$PATH"; fi
  fi
fi
if [[ "$platform" == "windows" ]] && ! command -v makensis >/dev/null 2>&1; then
  printf 'NSIS makensis was not found after checking PATH and ProgramFiles(x86).\n' >&2
  exit 1
fi

go run "$repository_root/tools/desktopconformance" -root "$repository_root" verify

temporary="$(mktemp -d "${TMPDIR:-/tmp}/layerdraw-desktop-installer.XXXXXX")"
preserved=(go.mod go.sum apps/desktop/wails.json apps/desktop/frontend/wailsjs/runtime/runtime.d.ts apps/desktop/frontend/wailsjs/runtime/runtime.js)
mkdir -p "$temporary/preserved"
for path in "${preserved[@]}"; do
  mkdir -p "$temporary/preserved/$(dirname "$path")"
  cp "$repository_root/$path" "$temporary/preserved/$path"
done
cleanup() {
  for path in "${preserved[@]}"; do cp "$temporary/preserved/$path" "$repository_root/$path"; done
  rm -rf "$repository_root/apps/desktop/build"
  rm -f "$repository_root/apps/desktop/frontend/dist/app.css" \
    "$repository_root/apps/desktop/frontend/dist/app.js" \
    "$repository_root/apps/desktop/frontend/dist/app.js.map" \
    "$repository_root/apps/desktop/frontend/wailsjs/runtime/package.json"
  rm -rf "$temporary"
}
trap cleanup EXIT
mkdir -p "$output" "$temporary/legal" "$temporary/runtime"

native_dir="$("$repository_root/tools/install-ladybug-native.sh")"
native_stage="$temporary/native"
mkdir -p "$native_stage"
install -m 0644 "$native_dir/libfts.lbug_extension" "$native_stage/"
install -m 0644 "$native_dir/libvector.lbug_extension" "$native_stage/"
install -m 0644 "$native_dir/libalgo.lbug_extension" "$native_stage/"
install -m 0644 "$native_dir/ladybug-native.json" "$native_stage/"
if [[ "$platform" == "windows" ]]; then install -m 0755 "$native_dir/lbug_shared.dll" "$native_stage/"; fi

node - "$repository_root/apps/desktop/wails.json" "$version" <<'NODE'
const fs = require("node:fs");
const path = process.argv[2];
const config = JSON.parse(fs.readFileSync(path, "utf8"));
config.info.productVersion = process.argv[3];
fs.writeFileSync(path, `${JSON.stringify(config, null, 2)}\n`);
NODE

corepack pnpm install --frozen-lockfile
if [[ "$platform" == "windows" ]]; then
  corepack pnpm --filter @layerdraw/engine-wasm exec tsc -p tsconfig.build.json
  # Keep dependency order explicit on Windows, where parallel workspace builds
  # can race while replacing dist directories. Filters with no matching package
  # are harmless, which keeps this compatible with branches predating review.
  for package in protocol composer render viewer engine-client client-sdk export registry-client library review react desktop; do
    corepack pnpm --filter "@layerdraw/$package" build
  done
else
  corepack pnpm exec turbo run build
fi

output_name="LayerDraw"
if [[ "$platform" == "windows" ]]; then output_name="LayerDraw.exe"; fi
build_args=(-clean -trimpath -s -skipbindings -o "$output_name")
if [[ "$platform" == "windows" ]]; then build_args+=(-nsis); fi
native_link_flags=""
if [[ "$platform" == "linux" ]]; then
  build_args+=(-tags webkit2_41,ladybug_native)
  native_link_flags="-Wl,--export-dynamic"
else
  build_args+=(-tags ladybug_native)
fi
(
  cd "$repository_root/apps/desktop"
  CGO_ENABLED=1 CGO_CFLAGS="-I$native_dir ${CGO_CFLAGS:-}" CGO_LDFLAGS="-L$native_dir $native_link_flags ${CGO_LDFLAGS:-}" \
    go run "github.com/wailsapp/wails/v2/cmd/wails@$wails_version" build "${build_args[@]}"
)

binary="$repository_root/apps/desktop/build/bin/LayerDraw"
if [[ "$platform" == "darwin" ]]; then
  app="$(find "$repository_root/apps/desktop/build/bin" -maxdepth 1 -type d -name '*.app' -print -quit)"
  if [[ -z "$app" ]]; then printf 'Wails did not produce a macOS application bundle.\n' >&2; exit 1; fi
  binary="$app/Contents/MacOS/LayerDraw"
fi
if [[ "$platform" == "windows" ]]; then
  binary="$binary.exe"
  nsis_binary="$binary"
  if command -v cygpath >/dev/null 2>&1; then nsis_binary="$(cygpath -w "$binary")"; fi
fi
fts_component="$native_stage/libfts.lbug_extension"
vector_component="$native_stage/libvector.lbug_extension"
algo_component="$native_stage/libalgo.lbug_extension"
if [[ "$platform" == "windows" ]] && command -v cygpath >/dev/null 2>&1; then
  fts_component="$(cygpath -w "$fts_component")"
  vector_component="$(cygpath -w "$vector_component")"
  algo_component="$(cygpath -w "$algo_component")"
fi
go run "$repository_root/tools/licensecheck" bundle -binary "$binary" -output "$temporary/legal" -version "$version" \
		-include-production-npm \
		-bundled-component "three|0.179.1|apps/desktop/frontend/dist/app.js|MIT|apps/desktop/node_modules/three/LICENSE|bfe119ea4fd413f5f7ca3fcd63adb0c4a073ed39daa2fe7d3e6b769e21272601" \
		-bundled-component "LadybugDB FTS extension|0.17.0|$fts_component|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0" \
		-bundled-component "LadybugDB Vector extension|0.17.0|$vector_component|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0" \
		-bundled-component "LadybugDB Algo extension|0.17.0|$algo_component|MIT|docs/legal/licenses/LadybugDB-MIT.txt|c7ac924b150ec18a9d9c7136a8cd533bcfa33109ea7b4b7712ea952a245186b0"
desktop_sbom="$temporary/legal/$(basename "$binary").cdx.json"
host="$temporary/runtime/layerdraw-host"
if [[ "$platform" == "windows" ]]; then host="$host.exe"; fi
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.releaseVersion=$version -X main.sourceRevision=${SOURCE_REVISION:-unknown}" \
  -o "$host" ./cmd/layerdraw-host
mkdir -p "$temporary/legal/host"
go run "$repository_root/tools/licensecheck" bundle -binary "$host" -output "$temporary/legal/host" -version "$version"
companion_sbom="$temporary/legal/host/$(basename "$host").cdx.json"
go run "$repository_root/tools/desktoprelease" merge-sbom \
  -desktop "$desktop_sbom" \
  -companion "$companion_sbom" \
  -output "$temporary/legal/LayerDraw-bundle.cdx.json"
cp "$repository_root/deploy/desktop-capabilities.json" "$temporary/legal/"
cp "$repository_root/deploy/desktop-conformance.json" "$temporary/legal/"

if [[ "$platform" == "windows" ]]; then
	if [[ "${LAYERDRAW_RELEASE_SIGNING:-0}" == "1" ]]; then
		: "${LAYERDRAW_WINDOWS_SIGNTOOL:?release signing requires LAYERDRAW_WINDOWS_SIGNTOOL}"
		: "${LAYERDRAW_WINDOWS_CERT_PATH:?release signing requires LAYERDRAW_WINDOWS_CERT_PATH}"
		: "${LAYERDRAW_WINDOWS_CERTIFICATE_PASSWORD:?release signing requires LAYERDRAW_WINDOWS_CERTIFICATE_PASSWORD}"
		for executable in "$binary" "$host"; do
			"$LAYERDRAW_WINDOWS_SIGNTOOL" sign /fd SHA256 /td SHA256 /tr https://timestamp.digicert.com \
				/f "$LAYERDRAW_WINDOWS_CERT_PATH" /p "$LAYERDRAW_WINDOWS_CERTIFICATE_PASSWORD" "$executable"
			"$LAYERDRAW_WINDOWS_SIGNTOOL" verify /pa "$executable"
		done
	fi
  installer_root="$repository_root/apps/desktop/build/windows/installer"
  mkdir -p "$installer_root/layerdraw-legal"
  cp -R "$temporary/legal/." "$installer_root/layerdraw-legal/"
  mkdir -p "$installer_root/layerdraw-runtime"
  cp "$host" "$installer_root/layerdraw-runtime/"
  mkdir -p "$installer_root/layerdraw-native"
  cp -R "$native_stage/." "$installer_root/layerdraw-native/"
  node - "$installer_root/project.nsi" <<'NODE'
const fs = require("node:fs");
const path = process.argv[2];
let source = fs.readFileSync(path, "utf8");
const anchor = '    CreateShortcut "$SMPROGRAMS\\${INFO_PRODUCTNAME}.lnk"';
const include = '!include "wails_tools.nsh"';
const legal = '    SetOutPath "$INSTDIR\\legal"\r\n    File /r "layerdraw-legal\\*"\r\n    SetOutPath "$INSTDIR\\runtime"\r\n    File /r "layerdraw-runtime\\*"\r\n    SetOutPath $INSTDIR\r\n\r\n';
const native = '    SetOutPath "$INSTDIR\\native"\r\n    File /r "layerdraw-native\\*"\r\n    SetOutPath $INSTDIR\r\n    File "layerdraw-native\\lbug_shared.dll"\r\n\r\n';
if (!source.includes(anchor) || !source.includes(include)) throw new Error("Wails NSIS template anchor was not found");
source = source.replace(include, '!define PRODUCT_EXECUTABLE "LayerDraw.exe"\r\n' + include);
  source = source.replace(anchor, legal + native + anchor);
fs.writeFileSync(path, source);
NODE
  (
    cd "$installer_root"
    makensis -DARG_WAILS_AMD64_BINARY="$nsis_binary" project.nsi
  )
fi

case "$platform" in
  darwin)
    mkdir -p "$app/Contents/Resources/layerdraw"
    cp -R "$temporary/legal/." "$app/Contents/Resources/layerdraw/"
    mkdir -p "$app/Contents/Resources/layerdraw/bin"
    cp "$host" "$app/Contents/Resources/layerdraw/bin/"
    mkdir -p "$app/Contents/Resources/layerdraw/native"
    cp -R "$native_stage/." "$app/Contents/Resources/layerdraw/native/"
    if [[ "${LAYERDRAW_RELEASE_SIGNING:-0}" == "1" ]]; then
      : "${LAYERDRAW_MACOS_SIGNING_IDENTITY:?release signing requires LAYERDRAW_MACOS_SIGNING_IDENTITY}"
      codesign --force --options runtime --timestamp --sign "$LAYERDRAW_MACOS_SIGNING_IDENTITY" "$app/Contents/Resources/layerdraw/bin/layerdraw-host"
      codesign --force --deep --options runtime --timestamp --sign "$LAYERDRAW_MACOS_SIGNING_IDENTITY" "$app"
      codesign --verify --deep --strict "$app"
    fi
    hdiutil create -quiet -fs HFS+ -srcfolder "$app" -volname "LayerDraw $version" "$output/LayerDraw-$version.dmg"
    ;;
  windows)
    installer="$(find "$repository_root/apps/desktop/build/bin" -maxdepth 1 -name '*-installer.exe' -print -quit)"
    if [[ -z "$installer" ]]; then printf 'Wails did not produce an NSIS installer.\n' >&2; exit 1; fi
    cp "$installer" "$output/LayerDraw-$version.exe"
    ;;
  linux)
    package_root="$temporary/deb"
    mkdir -p "$package_root/DEBIAN" "$package_root/usr/bin" "$package_root/usr/share/layerdraw/legal" "$package_root/usr/share/applications"
    install -m 0755 "$binary" "$package_root/usr/bin/layerdraw"
    mkdir -p "$package_root/usr/lib/layerdraw"
    install -m 0755 "$host" "$package_root/usr/lib/layerdraw/layerdraw-host"
    mkdir -p "$package_root/usr/lib/layerdraw/native"
    cp -R "$native_stage/." "$package_root/usr/lib/layerdraw/native/"
    cp -R "$temporary/legal/." "$package_root/usr/share/layerdraw/legal/"
    sed "s/@VERSION@/$version/g" "$repository_root/deploy/linux/layerdraw.control" > "$package_root/DEBIAN/control"
    cp "$repository_root/deploy/linux/layerdraw.desktop" "$package_root/usr/share/applications/"
    dpkg-deb --root-owner-group --build "$package_root" "$output/LayerDraw-$version.deb"
    ;;
esac

cp "$temporary/legal/LayerDraw-bundle.cdx.json" "$output/LayerDraw-$version.cdx.json"
cp "$temporary/legal/THIRD_PARTY_NOTICES.txt" "$output/LayerDraw-$version-THIRD_PARTY_NOTICES.txt"
cp "$repository_root/deploy/desktop-capabilities.json" "$output/LayerDraw-$version-capabilities.json"
cp "$repository_root/deploy/desktop-conformance.json" "$output/LayerDraw-$version-conformance.json"
