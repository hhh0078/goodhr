# 本脚本用于在 Windows 构建 GoodHR 控制台 Wails 桌面壳。
param(
  [string]$TargetOS = "windows",
  [string]$TargetArch = "amd64"
)

$ErrorActionPreference = "Stop"
$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$ShellDir = Join-Path $RootDir "console-shell"
$OutputName = "goodhr-console.exe"
if ($TargetOS -ne "windows") {
  $OutputName = "goodhr-console"
}

New-Item -ItemType Directory -Force -Path (Join-Path $ShellDir "bin") | Out-Null
Push-Location $ShellDir
try {
  Write-Host "构建 GoodHR 控制台壳：$TargetOS/$TargetArch"
  $env:GOOS = $TargetOS
  $env:GOARCH = $TargetArch
  go build -o (Join-Path $ShellDir "bin/$OutputName") .
  Copy-Item -Force (Join-Path $ShellDir "bin/$OutputName") (Join-Path $ShellDir $OutputName)
  Write-Host "输出文件：$(Join-Path $ShellDir "bin/$OutputName")"
} finally {
  Pop-Location
  Remove-Item Env:GOOS -ErrorAction SilentlyContinue
  Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
}
