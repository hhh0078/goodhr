# 文件作用：在 Windows 真机上检查 GoodHR Go 本地程序的基础接口、运行组件状态和诊断信息。
param(
  [string]$BaseUrl = "http://127.0.0.1:95271"
)

$ErrorActionPreference = "Stop"

# Write-Step 输出当前检查步骤。
# message 为中文步骤说明。
function Write-Step {
  param([string]$message)
  Write-Host "[GoodHR] $message" -ForegroundColor Cyan
}

# Invoke-GoodHRGet 请求本地程序接口并返回 JSON。
# path 为接口路径。
function Invoke-GoodHRGet {
  param([string]$path)
  $url = "$BaseUrl$path"
  Write-Step "GET $url"
  return Invoke-RestMethod -Method Get -Uri $url -TimeoutSec 10
}

Write-Step "开始检查 GoodHR Go 本地程序：$BaseUrl"

$health = Invoke-GoodHRGet "/health"
Write-Host "health.status = $($health.data.status)"
Write-Host "health.port   = $($health.data.port)"

$runtime = Invoke-GoodHRGet "/api/v1/runtime/status"
Write-Host "node_installed         = $($runtime.data.node_installed)"
Write-Host "worker_installed       = $($runtime.data.worker_installed)"
Write-Host "cloakbrowser_installed = $($runtime.data.cloakbrowser_installed)"

$worker = Invoke-GoodHRGet "/api/v1/worker/status"
Write-Host "worker.running = $($worker.data.running)"
Write-Host "worker.pid     = $($worker.data.pid)"

$diagnostics = Invoke-GoodHRGet "/api/v1/diagnostics"
Write-Host "diagnostics.os   = $($diagnostics.data.os)"
Write-Host "diagnostics.arch = $($diagnostics.data.arch)"
Write-Host "profile_locks    = $($diagnostics.data.profile_locks.Count)"

Write-Step "诊断建议"
foreach ($item in $diagnostics.data.recommendations) {
  Write-Host "- $item"
}

Write-Step "检查完成"
