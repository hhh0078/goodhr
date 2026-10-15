# 文件作用：仅在 Windows 本机准备并启动隔离的新手教程真实 Agent；复制程序和运行组件，不复制账号数据库、招聘登录资料或生产配置。
# 平台说明：这是 Windows 本地验收辅助脚本，不修改 Windows/macOS 正式安装包，也不代替 Mac 真机验收。
#Requires -Version 7.0
[CmdletBinding()]
param(
    [string]$PreviewRoot = 'D:\goodhr-onboarding-real-preview',
    [string]$InstalledRoot = 'D:\GoodHRLocalAgent',
    [string]$NodePath = 'C:\nvm4w\nodejs\node.exe'
)

$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw '此脚本仅供 Windows 本机验收使用。' }

# Get-ListeningProcess 返回指定回环端口的监听进程编号；没有监听时返回零。
function Get-ListeningProcess([int]$Port) {
    $listener = @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue)
    if ($listener.Count -gt 1) { throw "端口 $Port 有多个监听实例，请先检查环境。" }
    if ($listener.Count -eq 0) { return 0 }
    return [int]$listener[0].OwningProcess
}

# Copy-PreviewComponent 复制白名单中的组件目录并检查 Robocopy 返回码，不删除或移动任何源文件。
function Copy-PreviewComponent([string]$Source, [string]$Destination, [string]$LogPath) {
    if (-not (Test-Path -LiteralPath $Source -PathType Container)) { throw "组件目录不存在：$Source" }
    & robocopy.exe $Source $Destination /E /XJ /R:1 /W:1 /NFL /NDL /NP /NJH /NJS "/LOG+:$LogPath" | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "组件复制没有完成：$Source；详情见 $LogPath" }
}

# Assert-PreviewFile 通过 SHA256 确认隔离副本和源文件一致，避免运行未复制完整的组件。
function Assert-PreviewFile([string]$Source, [string]$Destination) {
    if (-not (Test-Path -LiteralPath $Destination -PathType Leaf)) { throw "隔离组件缺少文件：$Destination" }
    if ((Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash) {
        throw "组件校验没有通过：$Destination"
    }
}

# Get-PreviewRuntimeEvidence 检查隔离路径与组件就绪状态，只保存不含令牌的健康证据，不启动浏览器或招聘任务。
function Get-PreviewRuntimeEvidence([string]$ExpectedDataDir, [int]$ExpectedProcessId, [int]$ProductionProcessId) {
    $health = (Invoke-RestMethod 'http://127.0.0.1:43131/health' -TimeoutSec 5).data
    $runtime = (Invoke-RestMethod 'http://127.0.0.1:43131/api/v1/runtime/status' -TimeoutSec 10).data
    if ([IO.Path]::GetFullPath([string]$health.data_dir) -ne $ExpectedDataDir) { throw '隔离 Agent 返回的数据目录不正确。' }
    if ((Get-ListeningProcess 43131) -ne $ExpectedProcessId) { throw '隔离 Agent 监听进程发生变化。' }
    if ((Get-ListeningProcess 43129) -ne $ProductionProcessId) { throw '生产 Agent 监听进程发生变化，请检查环境。' }
    foreach ($key in @('node_installed', 'node_worker_installed', 'camoufox_installed', 'ocr_installed')) {
        if (-not $runtime.$key) { throw "隔离运行组件还没就绪：$key" }
    }
    if ($runtime.system_runtime.required -and -not $runtime.system_runtime.ready) { throw 'Windows 运行库还没就绪。' }
    if (-not $runtime.native_input_safety.ready) { throw '浏览器原生鼠标资源还没就绪。' }
    return [ordered]@{
        checked_at = [DateTimeOffset]::Now.ToString('o')
        process_id = $ExpectedProcessId
        production_process_id = $ProductionProcessId
        port = 43131
        worker_port = 39883
        cloud_api_base = 'http://127.0.0.1:8085'
        console_url = 'http://127.0.0.1:5174'
        health = $health
        runtime = $runtime
        browser_open_requested = $false
        recruiting_task_requested = $false
    }
}

$previewPath = [IO.Path]::GetFullPath($PreviewRoot).TrimEnd('\')
$installedPath = [IO.Path]::GetFullPath($InstalledRoot).TrimEnd('\')
if ($previewPath -notmatch '^D:\\[^\\]+') { throw '隔离目录必须位于 D 盘的独立子目录，避免占用 C 盘。' }
if ($previewPath.Equals($installedPath, [StringComparison]::OrdinalIgnoreCase) -or
    $previewPath.StartsWith($installedPath + '\', [StringComparison]::OrdinalIgnoreCase) -or
    $installedPath.StartsWith($previewPath + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw '隔离目录和生产安装目录不能相同，也不能互相包含。'
}
$dataPath = Join-Path $previewPath 'data'
$agentPath = Join-Path $previewPath 'goodhr-local-agent.exe'
$evidencePath = Join-Path $previewPath 'runtime-evidence.json'
$productionProcessId = Get-ListeningProcess 43129
$existingProcessId = Get-ListeningProcess 43131
if ($existingProcessId) {
    $existingProcess = Get-Process -Id $existingProcessId -ErrorAction Stop
    if ($existingProcess.Path -ne $agentPath) { throw '43131 已被其他程序占用，不会停止或修改它。' }
    $evidence = Get-PreviewRuntimeEvidence $dataPath $existingProcessId $productionProcessId
    $evidence | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $evidencePath -Encoding utf8
    $evidence | ConvertTo-Json -Depth 10
    return
}
if (Get-ListeningProcess 39883) { throw 'Worker 端口 39883 已被占用，不会复用或停止其他 Worker。' }
if ((Get-PSDrive D).Free -lt 3GB) { throw 'D 盘剩余空间不足 3GB，请先检查。' }
if (-not (Test-Path -LiteralPath $NodePath -PathType Leaf)) { throw '没有找到可只读复用的 Node.js。' }
if ((Test-Path -LiteralPath $previewPath) -and -not (Test-Path -LiteralPath (Join-Path $previewPath '.onboarding-real-preview'))) {
    throw '目标目录已经存在且没有本脚本标记，不会向其中写入。'
}
New-Item -ItemType Directory -Path $previewPath, $dataPath, (Join-Path $dataPath 'runtime'), (Join-Path $dataPath 'temp') -Force | Out-Null
'仅供 GoodHR 本机隔离验收；无生产数据库或招聘登录资料。' | Set-Content -LiteralPath (Join-Path $previewPath '.onboarding-real-preview') -Encoding utf8
$copyLog = Join-Path $previewPath 'component-copy.log'
$sourceAgent = Join-Path $installedPath 'goodhr-local-agent.exe'
Copy-Item -LiteralPath $sourceAgent -Destination $agentPath
Copy-PreviewComponent (Join-Path $installedPath 'worker') (Join-Path $previewPath 'worker') $copyLog
Copy-PreviewComponent (Join-Path $installedPath 'data\runtime\camoufox') (Join-Path $dataPath 'runtime\camoufox') $copyLog
$ocrSource = Join-Path $installedPath 'data\runtime\ocr'
if (-not (Test-Path -LiteralPath (Join-Path $ocrSource 'RapidOCR-json.exe'))) {
    $ocrSource = Join-Path $installedPath 'data\runtime\ocr.pre611-test'
}
Copy-PreviewComponent $ocrSource (Join-Path $dataPath 'runtime\ocr') $copyLog
Assert-PreviewFile $sourceAgent $agentPath
Assert-PreviewFile (Join-Path $installedPath 'worker\dist\main.js') (Join-Path $previewPath 'worker\dist\main.js')
Assert-PreviewFile (Join-Path $installedPath 'data\runtime\camoufox\camoufox.exe') (Join-Path $dataPath 'runtime\camoufox\camoufox.exe')
Assert-PreviewFile (Join-Path $ocrSource 'RapidOCR-json.exe') (Join-Path $dataPath 'runtime\ocr\RapidOCR-json.exe')

$previewEnvironment = @{
    GOODHR_CLOUD_API_BASE = 'http://127.0.0.1:8085'
    GOODHR_CONSOLE_URL = 'http://127.0.0.1:5174'
    GOODHR_WORKER_PORT = '39883'
    GOODHR_AUTO_OPEN_CONSOLE = 'false'
    GOODHR_NODE_PATH = $NodePath
    GOODHR_WORKER_ENTRY = (Join-Path $previewPath 'worker\dist\main.js')
    GOODHR_DATA_DIR = $dataPath
    GOODHR_OCR_EXECUTABLE = (Join-Path $dataPath 'runtime\ocr\RapidOCR-json.exe')
    CAMOUFOX_BINARY_PATH = (Join-Path $dataPath 'runtime\camoufox\camoufox.exe')
    TEMP = (Join-Path $dataPath 'temp')
    TMP = (Join-Path $dataPath 'temp')
    TMPDIR = (Join-Path $dataPath 'temp')
}
$previousEnvironment = @{}
try {
    foreach ($key in $previewEnvironment.Keys) {
        $previousEnvironment[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
        [Environment]::SetEnvironmentVariable($key, $previewEnvironment[$key], 'Process')
    }
    $agentProcess = Start-Process -FilePath $agentPath -ArgumentList @('--host', '127.0.0.1', '--port', '43131', '--data-dir', ('"' + $dataPath + '"')) -WorkingDirectory $previewPath -WindowStyle Hidden -RedirectStandardOutput (Join-Path $previewPath 'agent-stdout.log') -RedirectStandardError (Join-Path $previewPath 'agent-stderr.log') -PassThru
} finally {
    foreach ($key in $previousEnvironment.Keys) { [Environment]::SetEnvironmentVariable($key, $previousEnvironment[$key], 'Process') }
}
$deadline = [DateTime]::UtcNow.AddSeconds(50)
do {
    if ($agentProcess.HasExited) { throw "隔离 Agent 已退出，请查看 $previewPath\agent-stderr.log" }
    try {
        if ((Invoke-RestMethod 'http://127.0.0.1:43131/health' -TimeoutSec 2).ok) { break }
    } catch { }
    Start-Sleep -Milliseconds 500
} while ([DateTime]::UtcNow -lt $deadline)
$evidence = Get-PreviewRuntimeEvidence $dataPath $agentProcess.Id $productionProcessId
$evidence | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $evidencePath -Encoding utf8
$evidence | ConvertTo-Json -Depth 10
