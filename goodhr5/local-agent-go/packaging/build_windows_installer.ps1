# Purpose: build GoodHR Go Local Agent and create the Windows installer.
param(
  [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$DistInputDir = Join-Path $RootDir "dist\installer-input"
$SourceExe = Join-Path $RootDir "dist\bin\goodhr-local-agent-windows-amd64.exe"
$TargetExe = Join-Path $DistInputDir "goodhr-local-agent.exe"
$IssPath = Join-Path $PSScriptRoot "GoodHRLocalAgentGo.iss"

# Write-Step prints the current build step.
# message is the build step text.
function Write-Step {
  param([string]$message)
  Write-Host "[GoodHR] $message" -ForegroundColor Cyan
}

# Find-InnoSetup locates the Inno Setup compiler.
# Returns the ISCC.exe path.
function Find-InnoSetup {
  $candidates = @(
    "ISCC.exe",
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
  )
  foreach ($candidate in $candidates) {
    if (Get-Command $candidate -ErrorAction SilentlyContinue) {
      return (Get-Command $candidate).Source
    }
    if (Test-Path $candidate) {
      return $candidate
    }
  }
  throw "Inno Setup compiler ISCC.exe was not found. Please install Inno Setup 6 first."
}

Write-Step "Build Windows x64 Go local agent"
& (Join-Path $RootDir "scripts\build_go_binary.ps1") -TargetOS windows -TargetArch amd64 -Version $Version

Write-Step "Prepare installer input directory"
New-Item -ItemType Directory -Force -Path $DistInputDir | Out-Null
Copy-Item -Force $SourceExe $TargetExe
if (Test-Path (Join-Path $RootDir "worker-node")) {
  Remove-Item -Recurse -Force (Join-Path $DistInputDir "worker-node") -ErrorAction SilentlyContinue
  Copy-Item -Recurse -Force (Join-Path $RootDir "worker-node") (Join-Path $DistInputDir "worker-node")
}

$iscc = Find-InnoSetup
Write-Step "Create Windows installer"
& $iscc "/DMyAppVersion=$Version" $IssPath

$InstallerOutputDir = Join-Path $RootDir "dist-installer"
Write-Step "Installer build completed: $InstallerOutputDir"
