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
$probeStateKey = [guid]::NewGuid().ToString("N")
$probeStateRoot = Join-Path ([IO.Path]::GetTempPath()) ("layerdraw-desktop-probe-state-" + $probeStateKey)
New-Item -ItemType Directory -Force -Path $install, $data | Out-Null
$env:APPDATA = $data
$env:LAYERDRAW_DESKTOP_PROBE_STATE_KEY = $probeStateKey

function Invoke-Probe([string]$Executable, [string]$Action, [string]$Capabilities) {
  $env:LAYERDRAW_DESKTOP_PROBE_ACTION = $Action
  & $Executable --packaged-probe | Set-Content -Path $probe
  if ($LASTEXITCODE -ne 0) { throw "packaged probe failed during $Action" }
  go run ./tools/desktopprobe -input $probe -capabilities $Capabilities verify
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
  $companionHost = Join-Path $install "runtime\layerdraw-host.exe"
  if (-not (Test-Path $companionHost)) { throw "packaged MCP Host is missing" }
  if (-not (Test-Path (Join-Path $install "legal\desktop-capabilities.json"))) { throw "capability declaration is missing" }
  if (-not (Test-Path (Join-Path $install "legal\desktop-conformance.json"))) { throw "Desktop conformance declaration is missing" }
  if (-not (Test-Path (Join-Path $install "legal\host\layerdraw-host.exe.cdx.json"))) { throw "MCP Host SBOM is missing" }
  $developmentAssets = Get-ChildItem -Recurse -File $install | Where-Object { $_.Extension -eq ".map" -or $_.FullName -match "(testdata|test-fixtures)" }
  if ($developmentAssets) { throw "Windows installer contains development-only assets" }
  $capabilityDeclaration = Join-Path $install "legal\desktop-capabilities.json"
  Invoke-Probe $executable "initialize" $capabilityDeclaration

  $corrupt = Join-Path $root "corrupt.exe"
  [IO.File]::WriteAllBytes($corrupt, [IO.File]::ReadAllBytes($CurrentInstaller)[0..63])
  $corruptRejected = $false
  try {
    $failed = Start-Process -FilePath $corrupt -ArgumentList "/S" -Wait -PassThru
    $corruptRejected = $failed.ExitCode -ne 0
  } catch {
    # Windows rejecting the invalid executable is the expected rollback path.
    $corruptRejected = $true
  }
  if (-not $corruptRejected) { throw "corrupt installer was accepted" }
  if (-not (Test-Path $executable)) { throw "failed update removed the installed application" }
  Invoke-Probe $executable "verify" $capabilityDeclaration

  Install-LayerDraw $CurrentInstaller
  Invoke-Probe $executable "verify" $capabilityDeclaration
  if ($env:LAYERDRAW_EXPECT_SIGNED -eq "1") {
    foreach ($signedBinary in @($executable, $companionHost)) {
      if ((Get-AuthenticodeSignature $signedBinary).Status -ne "Valid") { throw "installed binary signature is not valid: $signedBinary" }
    }
  }
  if ($env:LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT) {
    $conformance = Start-Process -FilePath $executable -ArgumentList @("--packaged-conformance", $env:LAYERDRAW_DESKTOP_CONFORMANCE_OUTPUT) -NoNewWindow -Wait -PassThru
    if ($conformance.ExitCode -ne 0) { throw "installed Desktop conformance scenario failed: $($conformance.ExitCode)" }
  }
  $uninstaller = Join-Path $install "uninstall.exe"
  $removed = Start-Process -FilePath $uninstaller -ArgumentList "/S" -Wait -PassThru
  if ($removed.ExitCode -ne 0) { throw "uninstall failed: $($removed.ExitCode)" }
  if (Test-Path $executable) { throw "uninstall left the application executable behind" }
  if (-not (Test-Path (Join-Path $probeStateRoot "settings-v1.json"))) { throw "upgrade lost LayerDraw settings" }
  if (-not (Test-Path (Join-Path $probeStateRoot "projects\upgrade-probe\document.ldl"))) { throw "upgrade lost LayerDraw project data" }
} finally {
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $root
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $probeStateRoot
}
