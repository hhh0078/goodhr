# 文件作用说明：在 Windows Docker Desktop 上校验私有配置并构建启动内网系统，首次启动自动准备组件。
$ErrorActionPreference = "Stop"
$ProjectDir = Split-Path $PSScriptRoot -Parent
$EnvironmentFile = if ($env:GOODHR_ENV_FILE) { $env:GOODHR_ENV_FILE } else { Join-Path $ProjectDir "environments\private.env" }
if (-not (Test-Path -LiteralPath $EnvironmentFile)) { throw "请先复制 environments/private.env.example 为 private.env 并填写地址和密码" }
Push-Location $ProjectDir
try {
  $env:GOODHR_PRIVATE_BACKEND_DOCKERFILE_SHA256 = (Get-FileHash -Algorithm SHA256 deployment/private/Dockerfile).Hash.ToLowerInvariant()
  $env:GOODHR_PRIVATE_FRONTEND_DOCKERFILE_SHA256 = (Get-FileHash -Algorithm SHA256 cloud/frontend-next/Dockerfile).Hash.ToLowerInvariant()
  & python scripts/release_guard.py dockerfiles --backend deployment/private/Dockerfile
  if ($LASTEXITCODE -ne 0) { throw "镜像来源检查没有通过" }
  & docker compose --env-file $EnvironmentFile -f docker-compose.private.yml config --quiet
  if ($LASTEXITCODE -ne 0) { throw "私有部署配置检查没有通过" }
  & docker compose --env-file $EnvironmentFile -f docker-compose.private.yml run --rm extension-builder
  if ($LASTEXITCODE -ne 0) { throw "内网插件准备没有完成" }
  & docker compose --env-file $EnvironmentFile -f docker-compose.private.yml build backend frontend
  if ($LASTEXITCODE -ne 0) { throw "私有镜像构建没有完成" }
  & python scripts/release_guard.py image --kind backend --image goodhr-private-backend:local --dockerfile deployment/private/Dockerfile
  if ($LASTEXITCODE -ne 0) { throw "后端镜像来源检查没有通过" }
  & python scripts/release_guard.py image --kind frontend --image goodhr-private-frontend:local --dockerfile cloud/frontend-next/Dockerfile
  if ($LASTEXITCODE -ne 0) { throw "前端镜像来源检查没有通过" }
  & docker compose --env-file $EnvironmentFile -f docker-compose.private.yml up -d --no-build --wait --wait-timeout 3000
  if ($LASTEXITCODE -ne 0) { throw "启动尚未完成，请检查 backend 日志后重试" }
  & docker compose --env-file $EnvironmentFile -f docker-compose.private.yml cp gateway:/data/caddy/pki/authorities/local/root.crt private-data/GoodHR-private-root.crt
  if ($LASTEXITCODE -ne 0) { throw "系统已启动，但证书导出未成功，请按部署文档重新导出" }
  Write-Host "内网系统已启动。首次组件准备完成后，客户端可从内网下载。"
  Write-Host "请将 private-data/GoodHR-private-root.crt 分发到使用电脑，并按私有部署文档信任证书后访问。"
} finally { Pop-Location }
