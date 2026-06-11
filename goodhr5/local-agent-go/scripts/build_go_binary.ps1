# 文件作用：在 Windows 上编译 GoodHR Go 本地程序可执行文件，供发布包或安装器使用。
param(
  [string]$TargetOS = "windows",
  [string]$TargetArch = "amd64",
  [string]$Version = "go-v2-dev"
)

$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$DistDir = Join-Path $RootDir "dist\bin"
$Ext = ""
if ($TargetOS -eq "windows") {
  $Ext = ".exe"
}
$Output = Join-Path $DistDir "goodhr-local-agent-$TargetOS-$TargetArch$Ext"

# Write-Step 输出当前构建步骤。
# message 为中文步骤说明。
function Write-Step {
  param([string]$message)
  Write-Host "[GoodHR] $message" -ForegroundColor Cyan
}

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

Write-Step "开始编译 Go 本地程序：GOOS=$TargetOS GOARCH=$TargetArch"
Push-Location $RootDir
try {
  $env:CGO_ENABLED = "0"
  $env:GOOS = $TargetOS
  $env:GOARCH = $TargetArch
  go build -trimpath -ldflags="-s -w -X goodhr5/local-agent-go/internal/version.Value=$Version" -o $Output ./cmd/goodhr-local-agent
}
finally {
  Pop-Location
}

Write-Step "编译完成：$Output"
