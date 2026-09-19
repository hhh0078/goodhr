<!-- 本文件说明 GoodHR 本机开发、正式/测试环境的打包、部署、本地程序发布和回退步骤。 -->
# 分环境打包与部署

## 先选环境

| 命令模式 | 环境 | 网站/API 域名 | 后端端口 | 前端端口 |
| --- | --- | --- | --- | --- |
| 源码运行 | 本机开发 | 网站 `http://localhost:5173`，API `http://127.0.0.1:8084` | 8084 | 5173 |
| `build` | 正式 | `https://goodhr.58it.cn` | 8086 | 5175 |
| `dev` | 测试 | `https://goodhr-test.ai.58it.cn` | 8085 | 5174 |

**本地程序打包命令中的 `dev` 表示连接测试服务器，不表示 localhost。** 本机开发按下一节从源码启动，当前发布脚本只接受 `dev`、`build`，没有 `local` 模式。

公开配置统一保存在 `environments/production.env`、`environments/test.env`。测试域名绑定到宝塔已有 `goodhr.ai.58it.cn` 站点，保留该站点的根目录；无需新增测试站点。旧域名仍作为过渡别名，新的构建使用带 test 的域名。

## 本机开发：数据库、前后端与本地程序

实际启动顺序为：准备本地数据库 → 启动后端和前端 → 启动本地程序 → 打开控制台登录。下面的 Windows 路径按当前仓库位置填写；如果仓库放在其他位置，请替换路径。

### 1. 准备工具和本机配置

- Docker Desktop：启动本机前后端容器；本地程序在 Windows 或 macOS 宿主机运行。
- Go：满足各模块 `go.mod` 的版本要求，Node.js 22+；生成 Windows 安装器还需要 Inno Setup 6。
- 准备独立本地 PostgreSQL 数据库及账号。下面的 Compose 不会创建 PostgreSQL 或 Redis，也不会从服务器复制数据库。

```powershell
cd D:\work\good-hr-ai-js\goodhr5
if (-not (Test-Path .\cloud\backend\.env)) {
  Copy-Item .\cloud\backend\.env.example .\cloud\backend\.env
}
notepad .\cloud\backend\.env
```

按本机实际配置填写。示例中的尖括号是占位符，必须替换，不要提交真实密码：

```dotenv
GOODHR_CLOUD_ADDR=:8084
GOODHR_PUBLIC_BASE_URL=http://localhost:5173
GOODHR_PG_DSN=postgres://<本地数据库账号>:<URL编码后的本地密码>@host.docker.internal:5432/<本地数据库名>?sslmode=disable
GOODHR_REDIS_ADDR=
GOODHR_DISABLE_EMAIL_SCHEDULER=true
```

`host.docker.internal` 用于容器访问宿主机的数据库端口；若数据库在其他容器中，需要确保容器网络互通并填写对应地址。不要在这里填写正式数据库。示例文件中的旧端口应改成上面的 `8084`。

如果本机已有 PostgreSQL，但还没有开发库，可通过该实例的管理员 `psql` 会话创建一次（已有同名角色或库时不要重复执行）：

```sql
CREATE ROLE goodhr_dev LOGIN PASSWORD '<自行设置的本地密码>';
COMMENT ON ROLE goodhr_dev IS 'GoodHR 本机开发数据库账号';
CREATE DATABASE goodhr_dev OWNER goodhr_dev;
COMMENT ON DATABASE goodhr_dev IS 'GoodHR 本机开发数据库，与线上环境隔离';
```

随后把 DSN 中的账号和库名都填写为 `goodhr_dev`，密码使用上一步设置的值。业务表由后端迁移创建。

Redis 不配置时认证使用内存存储，重启后需重新登录；需要持续保留认证状态时配置本机 Redis。SMTP 未配置时使用开发邮件模式；需要真实验证码邮件时，补齐 `GOODHR_SMTP_HOST`、`GOODHR_SMTP_PORT`、`GOODHR_SMTP_USERNAME`、`GOODHR_SMTP_PASSWORD`、`GOODHR_SMTP_FROM`。数据库迁移随后端启动自动执行。

### 2. 构建并启动本机前后端

在 `goodhr5` 目录运行：

```powershell
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.yml up -d --build
docker compose -f docker-compose.yml ps
Invoke-RestMethod http://127.0.0.1:8084/health
```

前端地址：`http://localhost:5173/admin`。前端容器通过 `http://backend:8084` 访问后端，浏览器通过本机的 8084 端口访问后端。前端源文件变更会热更新；此配置下 Go 后端改代码后执行：

```powershell
docker compose -f docker-compose.yml restart backend
```

依赖文件或 Dockerfile 修改后重新执行 `up -d --build`。查看日志、停止环境：

```powershell
docker compose -f docker-compose.yml logs --tail 100 backend frontend
docker compose -f docker-compose.yml stop
```

仓库还保留 `docker-compose.local.yml` 和 `docker-compose.local.windows.yml` 两个热更新配置，它们依赖**已有**的 `data-services` 网络和配置指定的开发数据库。只有这些前提已准备好时才使用，例如 `docker compose -f docker-compose.local.windows.yml up -d --build`；不要与上面的配置同时启动，以免抢占 8084、5173 端口。

### 3. 在 Windows 启动本地程序源码

先停止已安装的本地程序及正在执行的招聘任务，避免它占用 43129 和 39881。新开一个 PowerShell 窗口：

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
$env:GOPROXY = 'https://goproxy.cn,direct'
npm config get registry
Push-Location .\worker
npm ci --registry=https://registry.npmmirror.com
if ($LASTEXITCODE -ne 0) { throw 'Worker 依赖安装失败' }
npm run build
if ($LASTEXITCODE -ne 0) { throw 'Worker 编译失败' }
Pop-Location

$env:GOODHR_CLOUD_API_BASE = 'http://127.0.0.1:8084'
$env:GOODHR_CONSOLE_URL = 'http://localhost:5173'
go run ./cmd/goodhr-local-agent
```

保持该终端运行。程序会打开本机控制台；登录并按页面提示准备运行组件，完成后再打开招聘浏览器。Worker 由本地 Go 程序管理，无需再手动运行 `npm start`。首次下载依赖当前本地数据库的 `system.onboarding_config.runtime_components`，需确保 Node、完整 Camoufox 包及所需 OCR 的 HTTPS 地址和 SHA256 已配置。

另开一个终端检查：

```powershell
Invoke-RestMethod http://127.0.0.1:43129/health
Invoke-RestMethod http://127.0.0.1:43129/api/v1/diagnostics
```

需要构建本机开发可执行文件时，在 Worker 编译完成后运行：

```powershell
New-Item -ItemType Directory -Force .\bin | Out-Null
go build -o .\bin\goodhr-local-agent.exe ./cmd/goodhr-local-agent
.\bin\goodhr-local-agent.exe
```

该文件用于当前源码目录调试，不是包含完整依赖的分发安装包。macOS 开发可在同一项目目录执行 `zsh scripts/build.sh`，再设置 `GOODHR_CLOUD_API_BASE=http://127.0.0.1:8084`、`GOODHR_CONSOLE_URL=http://localhost:5173` 并执行 `zsh scripts/run-dev.sh`。

### 4. 本机不使用 Docker 时

后端在 `cloud/backend` 目录运行 `go run ./cmd/server`，先在该终端设置 `GOODHR_PG_DSN`、`GOODHR_CLOUD_ADDR=:8084` 等环境变量。**Go 入口不会自动加载 `.env`**；直连本机数据库的地址应使用 `127.0.0.1`，不能照搬容器专用地址。

前端另开 PowerShell：

```powershell
cd D:\work\good-hr-ai-js\goodhr5\cloud\frontend-next
npm ci --registry=https://registry.npmmirror.com
$env:NEXT_PUBLIC_CLOUD_API_BASE = 'http://127.0.0.1:8084'
$env:CLOUD_API_BASE = 'http://127.0.0.1:8084'
$env:NEXT_PUBLIC_SITE_URL = 'http://localhost:5173'
npm run dev
```

只检查前端生产构建可在相同环境变量下运行 `npm run build`；构建成功后可运行 `npm run start -- --port 5173`。本地程序仍按上一节启动。

## 本地程序打包：Windows / macOS

打包前停止本机招聘任务，确认 Go、Node.js 22+、Inno Setup 6 已安装。脚本使用 npm 国内镜像和 Go 国内镜像；会重新安装并编译 Worker、生成 Go 程序、ZIP 和安装器。相同版本和环境的旧产物会被替换。

在 `goodhr5/local-agent-go-new` 目录执行，最后的 `build` 或 `dev` 必填：

```powershell
# 正式版 6.1.1
.\scripts\package-windows.ps1 -Version 6.1.1 build
# 测试版 6.1.1
.\scripts\package-windows.ps1 -Version 6.1.1 dev
# CMD 也可以使用批处理入口
.\scripts\package-windows.bat 6.1.1 build
.\scripts\package-windows.bat 6.1.1 dev
```

正式安装器：`release/GoodHR-Local-Agent-Setup-v6.1.1-windows-x64.exe`。
测试安装器：`release/GoodHR-Local-Agent-Setup-v6.1.1-test-windows-x64.exe`。
包内 `build-info.json` 记录版本、环境和域名。安装目录和本地监听端口沿用原有规则，同一台电脑需安装所需环境的版本，不能同时启动两种环境的本地程序。

macOS 在同一目录使用 `zsh scripts/package-release.sh 6.1.1 build`，测试版最后改为 `dev`。macOS 安装包需在 macOS 构建。

| 平台 / 环境 | `release/` 下的主要产物 |
| --- | --- |
| Windows 正式安装器 | `GoodHR-Local-Agent-Setup-v6.1.1-windows-x64.exe` |
| Windows 测试安装器 | `GoodHR-Local-Agent-Setup-v6.1.1-test-windows-x64.exe` |
| Windows 程序目录 ZIP | `goodhr-local-agent-v6.1.1-windows-x64.zip`，测试名增加 `-test` |
| macOS 程序目录 ZIP | `goodhr-local-agent-v6.1.1-darwin-arm64.zip` 或 `darwin-x64.zip`，测试名增加 `-test` |

打包后先查看发布目录中的 `build-info.json`，确认版本及域名；再安装对应 `.exe` 并启动，检查控制台域名、本地 `/health`、组件下载及浏览器打开。安装器、桌面快捷方式使用 `assets/goodhr-logo-transparent-512.png` 生成的图标；首次安装按 D → B → E → C 选择磁盘，升级保留原目录。

浏览器、Node 和 OCR 继续按后端组件配置下载。本次保留完整 Camoufox 字体包。

## 云端一键部署

服务器依赖 Docker Compose、Python 3、curl、运行中的 PostgreSQL 容器。前后端使用独立容器，镜像构建使用国内 Go/npm 镜像。

1. 分别准备正式、测试代码目录。将对应后端配置放在该目录的 `cloud/backend/.env`；也可通过绝对路径 `GOODHR_BACKEND_ENV` 指定。此文件不可提交。
2. 确认 `GOODHR_PG_DSN` 指向本环境数据库，Redis 配置隔离。设置 `GOODHR_DATABASE` 为该 DSN 中的数据库名称。数据库方案必须在首次正式切换前确定。
3. 将支付 PEM 放在公开环境文件中 `GOODHR_WECHAT_SECRET_DIR` 指定的服务器目录；容器内配置继续填写 `/run/secrets/wechat/文件名.pem`。不要将私钥粘贴进代码或镜像。
4. 在 `goodhr5` 目录执行：

```bash
# 正式环境：数据库名称按已确认方案填写
GOODHR_DATABASE=goodhr5 bash scripts/deploy-build.sh
# 测试环境沿用 goodhr6
GOODHR_DATABASE=goodhr6 bash scripts/deploy-dev.sh
# 只验证并构建镜像，不启动容器、不操作数据库
bash scripts/deploy-build.sh --build-only
bash scripts/deploy-dev.sh --build-only
```

部署脚本先构建镜像，备份指定 PostgreSQL 数据库和后端配置，再更新容器并检查后端 `/health` 和前端首页。备份放在 `backups/环境/时间/`，失败时不会自动删除旧镜像。PostgreSQL 容器名默认 `postgres`，可用 `GOODHR_POSTGRES_CONTAINER` 修改。脚本不会自动修改宝塔反代。

新后端启动会自动执行缺失的 SQL 迁移。正式站点切换前必须完成数据库备份和迁移验证。预热新容器时可设置 `GOODHR_DISABLE_EMAIL_SCHEDULER=true`，避免新旧后端同时发送定时提醒；切换完毕后只保留正式使用的实例执行定时任务。

宝塔正式域名的 API 反代切换到 `127.0.0.1:8086`，前端反代切换到 `127.0.0.1:5175`；保留原有上传路径、PHP 接口、TLS 等配置。测试站点仍使用 8085/5174。

旧正式环境存在自动更新 `main` 的计划任务。正式迁移完成前不要用推送 `main` 代替受控部署；切换后再调整该任务，使它调用对应环境脚本。

### 测试服务器完整操作

以下命令在服务器 SSH 终端执行，不是在 Windows PowerShell 执行：

```bash
cd /www/wwwroot/goodhr.ai.58it.cn/good-hr-ai-js
git status --short
git pull --ff-only origin main
cd goodhr5
GOODHR_DATABASE=goodhr6 bash scripts/deploy-dev.sh

docker compose --env-file environments/test.env -f docker-compose.dev.yml ps
docker compose --env-file environments/test.env -f docker-compose.dev.yml logs --tail 100 backend frontend
curl --fail http://127.0.0.1:8085/health
curl --fail https://goodhr-test.ai.58it.cn/api/system/local-agent-console-url
```

有未提交改动或 `git pull` 失败时，先处理冲突，不继续执行后续部署。成功后打开测试域名的 `/admin` 和 `/download`，使用 `dev` 模式生成的本地程序验证连接。测试环境使用数据库 `goodhr6`、Redis DB 6，不操作正式数据库；不会自动把正式数据再次复制到测试库。

### 正式服务器完整操作

```bash
cd /www/wwwroot/goodhr-production/good-hr-ai-js
git status --short
git pull --ff-only origin main
cd goodhr5
GOODHR_DATABASE=goodhr5 bash scripts/deploy-build.sh

docker compose --env-file environments/production.env -f docker-compose.build.yml ps
docker compose --env-file environments/production.env -f docker-compose.build.yml logs --tail 100 backend frontend
curl --fail http://127.0.0.1:8086/health
curl --fail https://goodhr.58it.cn/api/system/local-agent-console-url
```

正式环境继续使用原数据库 `goodhr5`、Redis DB 0。代码推送并不等于部署完成；当前需要执行上述脚本。不要启动已停用的旧正式容器。数据库名与 `.env` 中的 DSN 不一致时，部署脚本会在构建、备份和启动前拒绝执行。

### 配置与 PEM 放置位置

| 项目 | 正式环境 | 测试环境 |
| --- | --- | --- |
| 公开环境文件 | `environments/production.env` | `environments/test.env` |
| Compose 文件 | `docker-compose.build.yml` | `docker-compose.dev.yml` |
| 后端密码等配置 | 各自代码目录的 `cloud/backend/.env` | 各自代码目录的 `cloud/backend/.env` |
| 服务器 PEM 目录 | `/www/wwwroot/goodhr-production/secrets/wechat` | `/www/wwwroot/goodhr.ai.58it.cn/secrets/wechat` |
| 后端容器读取 PEM | `/run/secrets/wechat/实际文件名.pem` | `/run/secrets/wechat/实际文件名.pem` |
| 微信回调 | `https://goodhr.58it.cn/api/payment/notify/wechat` | `https://goodhr-test.ai.58it.cn/api/payment/notify/wechat` |

支付参数在对应环境的后台“系统配置”中维护，私钥和公钥字段填容器内路径。更换环境时分别检查支付回调、`system.onboarding_config.local_agent_console_url`、安装包地址和内置 AI 公开入口；构建脚本不会自动复制这些数据库配置。`.env`、PEM、数据库备份均不可提交 Git。

## 6.1.1 安装下载发布

OSS 站点根目录：`/www/wwwroot/oss2.58it.cn`。

面向用户下载的 ZIP 只放两个文件：`GoodHR-Local-Agent-Setup-v6.1.1-windows-x64.exe` 和该站点原有的 `安装教程图片.jpg`。

正式下载地址：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.1.1-windows-x64-5b2bd42f.zip`。

测试下载地址：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.1.1-test-windows-x64-c3dc864b.zip`。文件名最后八位为 ZIP SHA256 前缀，避免重新构建时命中 CDN 旧缓存。

验证 ZIP 和 HTTPS 下载后，在目标数据库 `system_configs` 的 `system.onboarding_config` JSON 中更新 `local_agent` 对应版本的 `url_win` 和 SHA256；保留 macOS 下载条目及全部 `runtime_components`。正式环境填写正式安装包，测试环境只能填写带 `-test` 的安装包，不能串用。

本地脚本生成的 `goodhr-local-agent-v*.zip` 是程序目录压缩包，与上述“安装器＋教程”的下载 ZIP 不同，发布时确认下载入口需要的文件类型。

后续发布顺序：生成安装器 → 上传至 OSS 站点根目录 → 与原有 `安装教程图片.jpg` 压缩 → 计算最终 ZIP 的 SHA256 → 给文件名加摘要前缀避免 CDN 旧缓存 → 更新对应环境的下载 JSON → 实际下载并核对摘要。示意 JSON（替换版本、地址和完整摘要）：

```json
{
  "version": "6.1.1",
  "url_win": "https://oss2.58it.cn/对应环境的安装教程包.zip",
  "url_mac": "",
  "sha256": "最终ZIP的64位SHA256",
  "note": "Windows 正式版或测试版，含安装教程"
}
```

将此条记录放到对应环境 `local_agent` 数组最前面；没有发布 macOS 新包时保留原有 macOS 记录。`sha256` 填最终用户下载 ZIP 的摘要，不填内部 EXE 的摘要。Windows 可用 `Get-FileHash -Algorithm SHA256 -LiteralPath '完整ZIP路径'`，Linux 可用 `sha256sum '完整ZIP路径'`。

## 常见问题与日志

| 现象 | 检查和处理 |
| --- | --- |
| 打包提示缺少模式 | 命令最后补 `dev` 或 `build`；版本号用 `-Version` 指定。 |
| PowerShell 显示乱码、缺少括号 | 保留脚本的 UTF-8 BOM，使用仓库里的原始脚本，勿从聊天记录重新粘贴整份脚本。 |
| 本机后端连不上数据库 | 检查 `.env`、容器网络和数据库权限；容器内 `127.0.0.1` 指的是容器自己。 |
| `go run` 没加载 `.env` | 在当前终端设置环境变量，或使用会加载 `env_file` 的 Compose 启动。 |
| 本机页面连不上本地程序 | 检查 43129 端口和 `/health`，停止原来已安装的实例后再启动源码程序。 |
| 部署提示备份库名不一致 | 对照本环境 DSN 中的数据库名修正 `GOODHR_DATABASE`，不要跳过备份。 |
| 网站 502 | 先检查对应后端 `/health`、前端端口与容器日志，再检查 nginx 上游；`nginx -t` 成功后才能重载。 |
| 下载校验失败 | 对照 JSON 中最终 ZIP 的摘要和实际下载文件，检查是否用了旧 CDN 文件名。 |

本地程序 Windows 日志：`%APPDATA%\GoodHR\local-agent-new\logs` 下的 `agent.log`、`worker.log`、`crash.log`；使用 `-data-dir` 时以 `/health` 返回的目录为准。云端后端日志在各自 `cloud/backend/logs/backend.log`，也可以使用上面的 Compose 日志命令。分享日志前需脱敏。

## 回退

先保存旧容器、镜像、nginx 配置和数据库备份。反代切换出现问题时恢复 nginx 备份，执行 `nginx -t` 后重载。数据库迁移后不能仅切回旧程序就认定兼容；需核对迁移影响，涉及数据库恢复时先停止写入并保留新数据，避免覆盖切换后产生的订单。

## 2026-09-19 部署记录

- 正式代码：`/www/wwwroot/goodhr-production/good-hr-ai-js/goodhr5`；容器 `goodhr-production-backend-1`、`goodhr-production-frontend-1`；继续使用原正式库 `goodhr5`，Redis DB 0。
- 测试代码仍在 `/www/wwwroot/goodhr.ai.58it.cn/good-hr-ai-js/goodhr5`；容器 `goodhr6-backend-1`、`goodhr6-frontend-1`；使用 `goodhr6`、Redis DB 6。测试环境关闭定时挽回邮件，登录验证码等主动邮件保留。
- 正式库副本成功执行 15 个迁移后，正式库升级至 `0080_payment_refunds.sql`；用户和订单数量核对无缺失。支付 PEM 以文件挂载，回调按环境分开。
- 正式及原正式别名 `goodhr5.58it.cn` 已转到新容器；原 PHP 和历史下载路径保留。测试新旧域名共用现有站点和双域名证书。
- 正式升级前备份：`/www/wwwroot/goodhr-production/backups/pre-6.1.1/`；最终停旧后端后的备份：`/www/wwwroot/goodhr-production/good-hr-ai-js/goodhr5/backups/production/20260919-125138/`。
- 测试部署备份：`/www/wwwroot/goodhr.ai.58it.cn/good-hr-ai-js/goodhr5/backups/test/20260919-125044/`；测试域名及证书备份在旧站点 `backups/domain-20260919/`。
- 旧正式容器已停止但保留。旧自动部署入口通过原项目 `.deploy.lock/release-owner` 暂停，防止它重新启用旧容器；当前使用上述新环境脚本部署。旧计划任务改为新部署入口之前不要删除该锁。
- Windows 正式与测试包均完成构建；PowerShell 5.1 解析正常，缺少模式会直接拒绝。后端与本地域名相关测试通过，云端页面/API、组件配置和公网下载摘要已核对。本机临时启动验收命令被执行策略拒绝，未将本机安装启动验收标记为完成；本次未构建 macOS 安装器。
