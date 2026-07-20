# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

param(
  [Parameter(Mandatory = $true)][string]$PreviousInstaller,
  [string]$CurrentInstaller = $PreviousInstaller
)

$ErrorActionPreference = "Stop"
$root = Join-Path $env:RUNNER_TEMP ("layerdraw-desktop-smoke-" + [guid]::NewGuid())
$install = Join-Path $root "LayerDraw"
$data = Join-Path $root "AppData"
$probe = Join-Path $root "probe.json"
New-Item -ItemType Directory -Force -Path $install, $data | Out-Null
$env:APPDATA = $data
$env:LAYERDRAW_DESKTOP_PROBE_STATE_ROOT = Join-Path $data "LayerDraw\upgrade-smoke"

function Invoke-Probe([string]$Executable, [string]$Action) {
  $env:LAYERDRAW_DESKTOP_PROBE_ACTION = $Action
  & $Executable --packaged-probe | Set-Content -Path $probe
  if ($LASTEXITCODE -ne 0) { throw "packaged probe failed during $Action" }
  go run ./tools/desktopprobe -input $probe verify
  if ($LASTEXITCODE -ne 0) { throw "packaged probe verification failed during $Action" }
}

function Install-LayerDraw([string]$Installer) {
  $arguments = @("/S", "/D=$install")
  $process = Start-Process -FilePath $Installer -ArgumentList $arguments -Wait -PassThru
  if ($process.ExitCode -ne 0) { throw "installer failed: $($process.ExitCode)" }
}

try {
  if ($env:LAYERDRAW_EXPECT_SIGNED -eq "1") {
    foreach ($installer in @($PreviousInstaller, $CurrentInstaller) | Select-Object -Unique) {
      if ((Get-AuthenticodeSignature $installer).Status -ne "Valid") { throw "installer signature is not valid: $installer" }
    }
  }
  Install-LayerDraw $PreviousInstaller
  $executable = Join-Path $install "LayerDraw.exe"
  $host = Join-Path $install "runtime\layerdraw-host.exe"
  if (-not (Test-Path $host)) { throw "packaged MCP Host is missing" }
  if (-not (Test-Path (Join-Path $install "legal\desktop-capabilities.json"))) { throw "capability declaration is missing" }
  if (-not (Test-Path (Join-Path $install "legal\host\layerdraw-host.cdx.json"))) { throw "MCP Host SBOM is missing" }
  $developmentAssets = Get-ChildItem -Recurse -File $install | Where-Object { $_.Extension -eq ".map" -or $_.FullName -match "(testdata|test-fixtures)" }
  if ($developmentAssets) { throw "Windows installer contains development-only assets" }
  Invoke-Probe $executable "initialize"

  $corrupt = Join-Path $root "corrupt.exe"
  [IO.File]::WriteAllBytes($corrupt, [IO.File]::ReadAllBytes($CurrentInstaller)[0..63])
  try {
    $failed = Start-Process -FilePath $corrupt -ArgumentList "/S" -Wait -PassThru
    if ($failed.ExitCode -eq 0) { throw "corrupt installer was accepted" }
  } catch [System.ComponentModel.Win32Exception] {
    # Windows rejecting the invalid executable is the expected rollback path.
  }
  if (-not (Test-Path $executable)) { throw "failed update removed the installed application" }
  Invoke-Probe $executable "verify"

  Install-LayerDraw $CurrentInstaller
  Invoke-Probe $executable "verify"
  if ($env:LAYERDRAW_EXPECT_SIGNED -eq "1") {
    foreach ($signedBinary in @($executable, $host)) {
      if ((Get-AuthenticodeSignature $signedBinary).Status -ne "Valid") { throw "installed binary signature is not valid: $signedBinary" }
    }
  }
  $uninstaller = Join-Path $install "uninstall.exe"
  $removed = Start-Process -FilePath $uninstaller -ArgumentList "/S" -Wait -PassThru
  if ($removed.ExitCode -ne 0) { throw "uninstall failed: $($removed.ExitCode)" }
  if (Test-Path $executable) { throw "uninstall left the application executable behind" }
  if (-not (Test-Path (Join-Path $env:LAYERDRAW_DESKTOP_PROBE_STATE_ROOT "settings-v1.json"))) { throw "upgrade lost LayerDraw settings" }
  if (-not (Test-Path (Join-Path $env:LAYERDRAW_DESKTOP_PROBE_STATE_ROOT "projects\upgrade-probe\document.ldl"))) { throw "upgrade lost LayerDraw project data" }
} finally {
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $root
}
