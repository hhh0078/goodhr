# Purpose: build GoodHR Go Local Agent and create the Windows installer.
param(
  [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$DistInputDir = Join-Path $RootDir "dist\installer-input"
$ConsoleInputDir = Join-Path $DistInputDir "console"
$SourceExe = Join-Path $RootDir "dist\bin\goodhr-local-agent-windows-amd64.exe"
$TargetExe = Join-Path $DistInputDir "goodhr-local-agent.exe"
$IssPath = Join-Path $PSScriptRoot "GoodHRLocalAgentGo.iss"
$FrontendDir = Resolve-Path (Join-Path $RootDir "..\cloud\frontend-next")
$FrontendOutDir = Join-Path $FrontendDir "out"

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

# Find-Npm locates npm.cmd to avoid PowerShell execution policy issues.
# Returns the npm.cmd path.
function Find-Npm {
  $npm = Get-Command "npm.cmd" -ErrorAction SilentlyContinue
  if ($npm) {
    return $npm.Source
  }
  $candidates = @(
    "$env:ProgramFiles\nodejs\npm.cmd",
    "${env:ProgramFiles(x86)}\nodejs\npm.cmd",
    "$env:APPDATA\npm\nnpm.cmd"
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      return $candidate
    }
  }
  throw "npm.cmd was not found. Please reinstall Node.js LTS or reopen PowerShell after installation."
}

# Ensure-NodeOnPath makes node.exe visible to npm child scripts.
# npm install may call "node install.js", so node.exe must be on PATH.
function Ensure-NodeOnPath {
  if (Get-Command "node.exe" -ErrorAction SilentlyContinue) {
    return
  }
  $candidates = @(
    "$env:ProgramFiles\nodejs\node.exe",
    "${env:ProgramFiles(x86)}\nodejs\node.exe"
  )
  foreach ($candidate in $candidates) {
    if (Test-Path $candidate) {
      $nodeDir = Split-Path $candidate -Parent
      $env:Path = "$nodeDir;$env:Path"
      Write-Step "Node added to PATH: $nodeDir"
      return
    }
  }
  throw "node.exe was not found. Please reinstall Node.js LTS or reopen PowerShell after installation."
}

Write-Step "Build Windows x64 Go local agent"
& (Join-Path $RootDir "scripts\build_go_binary.ps1") -TargetOS windows -TargetArch amd64 -Version $Version

Write-Step "Build local console frontend"
$npm = Find-Npm
Ensure-NodeOnPath
Push-Location $FrontendDir
try {
  if (!(Test-Path (Join-Path $FrontendDir "node_modules"))) {
    Write-Step "Install frontend dependencies"
    & $npm install
    if ($LASTEXITCODE -ne 0) {
      throw "Frontend npm install failed with exit code $LASTEXITCODE."
    }
  }
  $env:GOODHR_STATIC_EXPORT = "1"
  & $npm run build
  if ($LASTEXITCODE -ne 0) {
    throw "Frontend build failed with exit code $LASTEXITCODE."
  }
  if (!(Test-Path (Join-Path $FrontendOutDir "index.html"))) {
    throw "Frontend static export output was not found: $FrontendOutDir"
  }
}
finally {
  Remove-Item Env:\GOODHR_STATIC_EXPORT -ErrorAction SilentlyContinue
  Pop-Location
}

Write-Step "Prepare installer input directory"
New-Item -ItemType Directory -Force -Path $DistInputDir | Out-Null
Copy-Item -Force $SourceExe $TargetExe
if (Test-Path (Join-Path $RootDir "worker-node")) {
  Remove-Item -Recurse -Force (Join-Path $DistInputDir "worker-node") -ErrorAction SilentlyContinue
  Copy-Item -Recurse -Force (Join-Path $RootDir "worker-node") (Join-Path $DistInputDir "worker-node")
}
Remove-Item -Recurse -Force $ConsoleInputDir -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $ConsoleInputDir | Out-Null
Copy-Item -Recurse -Force (Join-Path $FrontendOutDir "*") $ConsoleInputDir

$iscc = Find-InnoSetup
Write-Step "Create Windows installer"
& $iscc "/DMyAppVersion=$Version" $IssPath
if ($LASTEXITCODE -ne 0) {
  throw "Inno Setup build failed with exit code $LASTEXITCODE."
}

$InstallerOutputDir = Join-Path $RootDir "dist-installer"
Write-Step "Installer build completed: $InstallerOutputDir"
