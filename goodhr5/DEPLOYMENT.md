<!-- 本文件说明 GoodHR 正式/测试环境的打包、部署、下载发布和回退步骤。 -->
# 分环境打包与部署

| 命令模式 | 环境 | 网站/API 域名 | 后端端口 | 前端端口 |
| --- | --- | --- | --- | --- |
| `build` | 正式 | `https://goodhr.58it.cn` | 8086 | 5175 |
| `dev` | 测试 | `https://goodhr-test.ai.58it.cn` | 8085 | 5174 |

公开配置统一保存在 `environments/production.env`、`environments/test.env`。测试域名绑定到宝塔已有 `goodhr.ai.58it.cn` 站点，保留该站点的根目录；无需新增测试站点。旧域名仍作为过渡别名，新的构建使用带 test 的域名。

## Windows 本地程序

在 `goodhr5/local-agent-go-new` 目录执行，最后的 `build` 或 `dev` 必填：

```powershell
# 正式版 6.1.1
.\scripts\package-windows.ps1 -Version 6.1.1 build
# 测试版 6.1.1
.\scripts\package-windows.ps1 -Version 6.1.1 dev
# CMD 也可以使用批处理入口
.\scripts\package-windows.bat 6.1.1 build
```

正式安装器：`release/GoodHR-Local-Agent-Setup-v6.1.1-windows-x64.exe`。
测试安装器：`release/GoodHR-Local-Agent-Setup-v6.1.1-test-windows-x64.exe`。
包内 `build-info.json` 记录版本、环境和域名。安装目录和本地监听端口沿用原有规则，同一台电脑需安装所需环境的版本，不能同时启动两种环境的本地程序。

macOS 在同一目录使用 `zsh scripts/package-release.sh 6.1.1 build`，测试版最后改为 `dev`。macOS 安装包需在 macOS 构建。

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

## 6.1.1 安装下载发布

OSS 站点根目录：`/www/wwwroot/oss2.58it.cn`。

面向用户下载的 ZIP 只放两个文件：`GoodHR-Local-Agent-Setup-v6.1.1-windows-x64.exe` 和该站点原有的 `安装教程图片.jpg`。

正式下载地址：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.1.1-windows-x64-5b2bd42f.zip`。

测试下载地址：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.1.1-test-windows-x64-c3dc864b.zip`。文件名最后八位为 ZIP SHA256 前缀，避免重新构建时命中 CDN 旧缓存。

验证 ZIP 和 HTTPS 下载后，在目标数据库 `system_configs` 的 `system.onboarding_config` JSON 中更新 `local_agent` 对应版本的 `url_win` 和 SHA256；保留 macOS 下载条目及全部 `runtime_components`。正式环境填写正式安装包，测试环境只能填写带 `-test` 的安装包，不能串用。

本地脚本生成的 `goodhr-local-agent-v*.zip` 是程序目录压缩包，与上述“安装器＋教程”的下载 ZIP 不同，发布时确认下载入口需要的文件类型。

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
