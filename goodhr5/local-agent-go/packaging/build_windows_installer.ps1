# 文件作用：在 Windows 上编译 GoodHR Go 本地程序并生成 Inno Setup 安装器。
param(
  [string]$Version = "0.1.0"
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$DistInputDir = Join-Path $RootDir "dist\installer-input"
$SourceExe = Join-Path $RootDir "dist\bin\goodhr-local-agent-windows-amd64.exe"
$TargetExe = Join-Path $DistInputDir "goodhr-local-agent.exe"
$IssPath = Join-Path $PSScriptRoot "GoodHRLocalAgentGo.iss"

# Write-Step 输出当前构建步骤。
# message 为中文步骤说明。
function Write-Step {
  param([string]$message)
  Write-Host "[GoodHR] $message" -ForegroundColor Cyan
}

# Find-InnoSetup 查找 Inno Setup 编译器。
# 返回 ISCC.exe 路径。
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
  throw "未找到 Inno Setup 编译器 ISCC.exe，请先安装 Inno Setup 6。"
}

Write-Step "编译 Windows x64 Go 本地程序"
& (Join-Path $RootDir "scripts\build_go_binary.ps1") -TargetOS windows -TargetArch amd64

Write-Step "准备安装器输入目录"
New-Item -ItemType Directory -Force -Path $DistInputDir | Out-Null
Copy-Item -Force $SourceExe $TargetExe

$iscc = Find-InnoSetup
Write-Step "生成 Windows 安装器"
& $iscc "/DMyAppVersion=$Version" $IssPath

Write-Step "安装器生成完成：$(Join-Path $RootDir "dist-installer")"
