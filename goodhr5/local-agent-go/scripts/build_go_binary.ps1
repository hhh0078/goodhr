# Purpose: build the GoodHR Go Local Agent executable for release or installer packaging.
param(
  [string]$TargetOS = "windows",
  [string]$TargetArch = "amd64",
  [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$DistDir = Join-Path $RootDir "dist\bin"
$Ext = ""
if ($TargetOS -eq "windows") {
  $Ext = ".exe"
}
$Output = Join-Path $DistDir "goodhr-local-agent-$TargetOS-$TargetArch$Ext"
$SubsystemFlag = ""
if ($TargetOS -eq "windows") {
  $SubsystemFlag = "-H windowsgui "
}

# Write-Step prints the current build step.
# message is the build step text.
function Write-Step {
  param([string]$message)
  Write-Host "[GoodHR] $message" -ForegroundColor Cyan
}

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

Write-Step "Build Go local agent: GOOS=$TargetOS GOARCH=$TargetArch"
Push-Location $RootDir
try {
  $env:CGO_ENABLED = "0"
  $env:GOOS = $TargetOS
  $env:GOARCH = $TargetArch
  go build -trimpath -ldflags="$SubsystemFlag-s -w -X goodhr5/local-agent-go/internal/version.Value=$Version" -o $Output ./cmd/goodhr-local-agent
}
finally {
  Pop-Location
}

Write-Step "Build completed: $Output"
