# Desktop release and update contract

LayerDraw Desktop publishes installable artifacts only from an annotated `v*` tag through
`.github/workflows/desktop-release.yml`. GitHub Releases is the canonical distribution and
update channel. App Store, Microsoft Store, and Linux repository packaging are optional
follow-ups; they must consume the same verified artifacts and must not replace or weaken this
contract.

## Declared installers

| Platform | Installer | Platform signature |
| --- | --- | --- |
| macOS | DMG containing the notarized `.app` | Apple Developer ID, hardened runtime, notarization and stapling |
| Windows | NSIS `.exe` with embedded WebView2 bootstrap | Authenticode with RFC 3161 timestamp |
| Linux | Debian `.deb` plus detached armored signature | release GPG key and signed update metadata |

The Wails CLI is pinned to `v2.12.0`, while Go is pinned by `.go-version`. Each installer
contains the Desktop executable, the companion MCP Host/runtime, the packaged capability
declaration, CycloneDX documents, license texts, and third-party notices. The build script
removes generated source maps and Wails development metadata before returning.

## Secret isolation and fail-closed behavior

Pull requests use a newly generated, process-local Ed25519 test key. Test-signed metadata is
rejected unless verification explicitly opts in. No private test or release key is stored in
the repository or uploaded as an artifact.

Tagged release jobs fail before packaging when the credentials for their platform are absent:

- `LAYERDRAW_MACOS_CERTIFICATE`, `LAYERDRAW_MACOS_CERTIFICATE_PASSWORD`,
  `LAYERDRAW_MACOS_SIGNING_IDENTITY`, `LAYERDRAW_APPLE_ID`,
  `LAYERDRAW_APPLE_APP_PASSWORD`, and `LAYERDRAW_APPLE_TEAM_ID`;
- `LAYERDRAW_WINDOWS_CERTIFICATE` and `LAYERDRAW_WINDOWS_CERTIFICATE_PASSWORD`;
- `LAYERDRAW_LINUX_GPG_PRIVATE_KEY` and `LAYERDRAW_LINUX_GPG_PASSPHRASE`;
- `LAYERDRAW_DESKTOP_UPDATE_SIGNING_KEY` and its independently configured trust anchor,
  `LAYERDRAW_DESKTOP_UPDATE_PUBLIC_KEY`.

Certificates and keys are exposed only in the tag/manual-only release workflow, guarded by the
protected `desktop-release` environment. Pull requests run the separate installer CI workflow,
which contains no production secret references. The update key is exposed only to metadata
creation. Checkout credentials are disabled, and only the final release job receives
`contents: write`.

## Update policy

Every platform has a signed JSON manifest that binds the installer SHA-256 digest and size,
any detached platform signature, artifact-specific SBOM and notices, packaged capabilities,
source revision, build workflow, timestamp, channel, target version, and minimum compatible installed version. Verification
rejects unknown fields, an untrusted key, altered metadata, altered payloads, platform/channel
mismatch, downgrade/reinstall, and clients older than the declared compatibility floor.

The workflow exercises a fresh install and launch, a same-artifact upgrade while preserving a
user-data marker, rejection of a truncated installer, and uninstall on every supported runner.
Failed validation never replaces the installed application. Rollback of an already-installed
version therefore means retaining the prior installation and its user data; automatic
downgrades are intentionally forbidden.

## Packaged security boundary

`deploy/desktop-capabilities.json` is part of the signed payload. It declares required runtime
components and asserts that provider credentials, signing secrets, or preconfigured MCP
endpoints are absent. Development servers, test fixtures, and source maps are excluded. The
release workflow publishes the installer, detached platform signature where applicable, signed
update manifest, SBOMs, capability declaration, notices, and provenance together.
