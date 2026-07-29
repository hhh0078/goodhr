# 文件作用说明：在 Windows x64 上构建 Go 主程序、严格 TypeScript Worker、ZIP 发布包和 Inno Setup 安装器。
param(
  [string]$Version = "6"
)

$ErrorActionPreference = "Stop"

$ProjectDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$WorkerDir = Join-Path $ProjectDir "worker"
$ReleaseRoot = Join-Path $ProjectDir "release"
$PackageName = "goodhr-local-agent-v$Version-windows-x64"
$PackageDir = Join-Path $ReleaseRoot $PackageName
$ArchivePath = Join-Path $ReleaseRoot "$PackageName.zip"
$InstallerScript = Join-Path $ProjectDir "packaging\GoodHRLocalAgent.iss"
$NpmRegistry = if ($env:GOODHR_NPM_REGISTRY) { $env:GOODHR_NPM_REGISTRY } else { "https://registry.npmmirror.com" }
$GoProxy = if ($env:GOPROXY) { $env:GOPROXY } else { "https://goproxy.cn,direct" }

if ($Version -notmatch '^[0-9A-Za-z._-]+$') {
  throw "版本号只能包含数字、字母、点、下划线和短横线"
}

# Write-Step 输出当前 Windows 打包步骤。
function Write-Step {
  param([string]$Message)
  Write-Host "[GoodHR] $Message" -ForegroundColor Cyan
}

# Find-CommandPath 返回第一个可用命令的完整路径。
function Find-CommandPath {
  param([string[]]$Names)
  foreach ($Name in $Names) {
    $Command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($Command) {
      return $Command.Source
    }
  }
  throw "没有找到命令：$($Names -join ', ')"
}

# Find-InnoSetup 返回 Inno Setup 6 编译器路径。
function Find-InnoSetup {
  $Candidates = @(
    "ISCC.exe",
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
  )
  foreach ($Candidate in $Candidates) {
    $Command = Get-Command $Candidate -ErrorAction SilentlyContinue
    if ($Command) {
      return $Command.Source
    }
    if ($Candidate -and (Test-Path $Candidate)) {
      return $Candidate
    }
  }
  throw "没有找到 Inno Setup 6，请安装后再生成 Windows 安装器"
}

$Npm = Find-CommandPath @("npm.cmd", "npm")
$Go = Find-CommandPath @("go.exe", "go")
$InnoSetup = Find-InnoSetup

Write-Step "清理本次 Windows 发布目录"
if (Test-Path $PackageDir) {
  Remove-Item -Recurse -Force $PackageDir
}
if (Test-Path $ArchivePath) {
  Remove-Item -Force $ArchivePath
}
New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "worker") | Out-Null

Write-Step "编译严格 TypeScript Worker"
Push-Location $WorkerDir
try {
  & $Npm ci --registry=$NpmRegistry
  if ($LASTEXITCODE -ne 0) {
    throw "Worker 依赖安装失败，退出码：$LASTEXITCODE"
  }
  & $Npm run build
  if ($LASTEXITCODE -ne 0) {
    throw "Worker 编译失败，退出码：$LASTEXITCODE"
  }
  Copy-Item -Recurse -Force "dist" (Join-Path $PackageDir "worker\dist")
  Copy-Item -Force "package.json", "package-lock.json" (Join-Path $PackageDir "worker")
}
finally {
  Pop-Location
}

Write-Step "安装 Worker 生产依赖"
Push-Location (Join-Path $PackageDir "worker")
try {
  & $Npm ci --omit=dev --registry=$NpmRegistry
  if ($LASTEXITCODE -ne 0) {
    throw "Worker 生产依赖安装失败，退出码：$LASTEXITCODE"
  }
}
finally {
  Pop-Location
}

Write-Step "编译 Windows x64 Go 主程序"
Push-Location $ProjectDir
try {
  $PreviousCGO = $env:CGO_ENABLED
  $PreviousGOOS = $env:GOOS
  $PreviousGOARCH = $env:GOARCH
  $PreviousGOPROXY = $env:GOPROXY
  $env:CGO_ENABLED = "0"
  $env:GOOS = "windows"
  $env:GOARCH = "amd64"
  $env:GOPROXY = $GoProxy
  $Ldflags = "-H windowsgui -s -w -X goodhr5/local-agent-go-new/internal/version.Value=$Version -X goodhr5/local-agent-go-new/internal/config.DefaultCloudURL=https://goodhr5.58it.cn -X goodhr5/local-agent-go-new/internal/config.DefaultConsoleURL=https://goodhr5.58it.cn"
  & $Go build -trimpath -ldflags=$Ldflags -o (Join-Path $PackageDir "goodhr-local-agent.exe") .\cmd\goodhr-local-agent
  if ($LASTEXITCODE -ne 0) {
    throw "Windows Go 主程序编译失败，退出码：$LASTEXITCODE"
  }
}
finally {
  $env:CGO_ENABLED = $PreviousCGO
  $env:GOOS = $PreviousGOOS
  $env:GOARCH = $PreviousGOARCH
  $env:GOPROXY = $PreviousGOPROXY
  Pop-Location
}

Copy-Item -Force (Join-Path $ProjectDir "README.md") (Join-Path $PackageDir "README.md")

Write-Step "生成 Windows ZIP 发布包"
Compress-Archive -Path $PackageDir -DestinationPath $ArchivePath -CompressionLevel Optimal

Write-Step "生成 Inno Setup 安装器"
& $InnoSetup "/DMyAppVersion=$Version" $InstallerScript
if ($LASTEXITCODE -ne 0) {
  throw "Windows 安装器生成失败，退出码：$LASTEXITCODE"
}

Write-Step "Windows 发布包已生成：$ArchivePath"

