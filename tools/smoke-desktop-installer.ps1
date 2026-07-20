# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

param(
  [Parameter(Mandatory = $true)][string]$Installer
)

$ErrorActionPreference = "Stop"
$root = Join-Path $env:RUNNER_TEMP ("layerdraw-desktop-smoke-" + [guid]::NewGuid())
$install = Join-Path $root "LayerDraw"
$data = Join-Path $root "AppData"
$probe = Join-Path $root "probe.json"
New-Item -ItemType Directory -Force -Path $install, $data | Out-Null
$env:APPDATA = $data
$marker = Join-Path $data "LayerDraw\upgrade-marker"
New-Item -ItemType Directory -Force -Path (Split-Path $marker) | Out-Null
Set-Content -NoNewline -Path $marker -Value "preserve-me"

try {
  if ($env:LAYERDRAW_EXPECT_SIGNED -eq "1" -and (Get-AuthenticodeSignature $Installer).Status -ne "Valid") { throw "installer signature is not valid" }
  $arguments = @("/S", "/D=$install")
  $first = Start-Process -FilePath $Installer -ArgumentList $arguments -Wait -PassThru
  if ($first.ExitCode -ne 0) { throw "fresh install failed: $($first.ExitCode)" }
  $executable = Join-Path $install "LayerDraw.exe"
  & $executable --packaged-probe | Set-Content -Path $probe
  if (-not (Test-Path (Join-Path $install "runtime\layerdraw-host.exe"))) { throw "packaged MCP Host is missing" }
  if (-not (Test-Path (Join-Path $install "legal\desktop-capabilities.json"))) { throw "capability declaration is missing" }
  if (-not (Test-Path (Join-Path $install "legal\host\layerdraw-host.cdx.json"))) { throw "MCP Host SBOM is missing" }
  $developmentAssets = Get-ChildItem -Recurse -File $install | Where-Object { $_.Extension -eq ".map" -or $_.FullName -match "(testdata|test-fixtures)" }
  if ($developmentAssets) { throw "Windows installer contains development-only assets" }
  if ($env:LAYERDRAW_EXPECT_SIGNED -eq "1") {
    foreach ($signedBinary in @($executable, (Join-Path $install "runtime\layerdraw-host.exe"))) {
      if ((Get-AuthenticodeSignature $signedBinary).Status -ne "Valid") { throw "installed binary signature is not valid: $signedBinary" }
    }
  }

  $upgrade = Start-Process -FilePath $Installer -ArgumentList $arguments -Wait -PassThru
  if ($upgrade.ExitCode -ne 0) { throw "upgrade failed: $($upgrade.ExitCode)" }
  if ((Get-Content -Raw $marker) -ne "preserve-me") { throw "upgrade did not preserve user data" }

  $corrupt = Join-Path $root "corrupt.exe"
  [IO.File]::WriteAllBytes($corrupt, [IO.File]::ReadAllBytes($Installer)[0..63])
  try {
    $failed = Start-Process -FilePath $corrupt -ArgumentList "/S" -Wait -PassThru
    if ($failed.ExitCode -eq 0) { throw "corrupt installer was accepted" }
  } catch [System.ComponentModel.Win32Exception] {
    # Windows rejecting the invalid executable is the expected rollback path.
  }
  if (-not (Test-Path $executable)) { throw "failed update removed the installed application" }
  if ((Get-Content -Raw $marker) -ne "preserve-me") { throw "failed update damaged user data" }

  $uninstaller = Join-Path $install "uninstall.exe"
  $removed = Start-Process -FilePath $uninstaller -ArgumentList "/S" -Wait -PassThru
  if ($removed.ExitCode -ne 0) { throw "uninstall failed: $($removed.ExitCode)" }
  if (Test-Path $executable) { throw "uninstall left the application executable behind" }
  go run ./tools/desktopprobe -input $probe verify
} finally {
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $root
}
