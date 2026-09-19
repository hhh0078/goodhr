# 文件作用说明：在 Windows x64 上构建 Go 主程序、严格 TypeScript Worker、ZIP 发布包和 Inno Setup 安装器。
[CmdletBinding(PositionalBinding = $false)]
param(
  [string]$Version = "6",
  [Parameter(Position = 0)]
  [ValidateSet("dev", "build")]
  [string]$Mode
)

$ErrorActionPreference = "Stop"
if (-not $Mode) { throw "请在打包命令后指定 dev（测试）或 build（正式），例如：-Version 6.1.1 build" }
$Environment = if ($Mode -eq "build") { "production" } else { "test" }

$ProjectDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$WorkerDir = Join-Path $ProjectDir "worker"
$ReleaseRoot = Join-Path $ProjectDir "release"
$EnvironmentSuffix = if ($Environment -eq "test") { "-test" } else { "" }
$PackageName = "goodhr-local-agent-v$Version$EnvironmentSuffix-windows-x64"
$PackageDir = Join-Path $ReleaseRoot $PackageName
$ArchivePath = Join-Path $ReleaseRoot "$PackageName.zip"
$InstallerPath = Join-Path $ReleaseRoot "GoodHR-Local-Agent-Setup-v$Version$EnvironmentSuffix-windows-x64.exe"
$LegacyInstallerPath = Join-Path $ReleaseRoot "GoodHRLocalAgentSetup-$Version-windows-x64.exe"
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

$EnvironmentFile = Join-Path $ProjectDir "..\environments\$Environment.env"
if (-not (Test-Path -LiteralPath $EnvironmentFile)) { throw "没有找到环境配置：$EnvironmentFile" }
$DomainLine = Get-Content -LiteralPath $EnvironmentFile | Where-Object { $_ -match '^GOODHR_PUBLIC_BASE_URL=' } | Select-Object -First 1
if (-not $DomainLine) { throw "环境配置缺少 GOODHR_PUBLIC_BASE_URL" }
$PublicBaseURL = ($DomainLine -split '=', 2)[1].Trim().TrimEnd('/')
if ($PublicBaseURL -notmatch '^https://[a-z0-9.-]+$') { throw "环境主域名格式不正确" }

$Npm = Find-CommandPath @("npm.cmd", "npm")
$Node = Find-CommandPath @("node.exe", "node")
$Go = Find-CommandPath @("go.exe", "go")
$InnoSetup = Find-InnoSetup

Write-Step "从项目 PNG Logo 生成 Windows 图标"
& (Join-Path $PSScriptRoot "generate-windows-icon.ps1")

Write-Step "清理本次 Windows 发布目录"
$ResolvedReleaseRoot = [IO.Path]::GetFullPath($ReleaseRoot).TrimEnd('\') + '\'
foreach ($TargetPath in @($PackageDir, $ArchivePath, $InstallerPath, $LegacyInstallerPath)) {
  if (-not [IO.Path]::GetFullPath($TargetPath).StartsWith($ResolvedReleaseRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "打包清理路径超出 release 目录"
  }
}
if (Test-Path $PackageDir) {
  Remove-Item -Recurse -Force $PackageDir
}
if (Test-Path $ArchivePath) {
  Remove-Item -Force $ArchivePath
}
if (Test-Path $InstallerPath) {
  Remove-Item -Force $InstallerPath
}
if (Test-Path $LegacyInstallerPath) {
  Remove-Item -Force $LegacyInstallerPath
}
New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "worker") | Out-Null

Write-Step "编译严格 TypeScript Worker"
Push-Location $WorkerDir
try {
  # better-sqlite3 已自带 Windows 预编译文件；跳过其隐式 node-gyp，避免打包机必须安装 Visual Studio。
  & $Npm ci --ignore-scripts --registry=$NpmRegistry
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
  & $Npm ci --omit=dev --ignore-scripts --registry=$NpmRegistry
  if ($LASTEXITCODE -ne 0) {
    throw "Worker 生产依赖安装失败，退出码：$LASTEXITCODE"
  }
  # 验证预编译 SQLite 模块确实可加载，防止跳过脚本后打出不可运行的安装包。
  & $Node -e "const Database=require('better-sqlite3');const db=new Database(':memory:');db.prepare('SELECT 1').get();db.close()"
  if ($LASTEXITCODE -ne 0) {
    throw "Worker 生产依赖中的 SQLite 模块不可用，退出码：$LASTEXITCODE"
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
  $Ldflags = "-H windowsgui -s -w -X goodhr5/local-agent-go-new/internal/version.Value=$Version -X goodhr5/local-agent-go-new/internal/config.DefaultCloudURL=$PublicBaseURL -X goodhr5/local-agent-go-new/internal/config.DefaultConsoleURL=$PublicBaseURL"
  & $Go build -trimpath -ldflags $Ldflags -o (Join-Path $PackageDir "goodhr-local-agent.exe") .\cmd\goodhr-local-agent
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
@{ version = $Version; environment = $Environment; public_base_url = $PublicBaseURL } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $PackageDir "build-info.json") -Encoding UTF8

Write-Step "生成 Windows ZIP 发布包"
Compress-Archive -Path $PackageDir -DestinationPath $ArchivePath -CompressionLevel Optimal

Write-Step "生成 Inno Setup 安装器"
& $InnoSetup "/Q" "/DMyAppVersion=$Version" "/DMyEnvironmentSuffix=$EnvironmentSuffix" $InstallerScript
if ($LASTEXITCODE -ne 0) {
  throw "Windows 安装器生成失败，退出码：$LASTEXITCODE"
}

Write-Step "Windows 发布包已生成：$ArchivePath"

