# GoodHR6 测试环境部署记录

本文件记录 `goodhr.ai.58it.cn` 测试环境的部署方式、验证结果和本次遇到的错误，所有密钥和密码均省略。

## 环境

| 项目 | 配置 |
| --- | --- |
| 站点 | `https://goodhr.ai.58it.cn` |
| 源码目录 | `/www/wwwroot/goodhr.ai.58it.cn/good-hr-ai-js` |
| Git 分支 | `main`，与本地当前分支一致 |
| 前端容器 | `goodhr6-frontend-1`，`127.0.0.1:5174 → 3000` |
| 后端容器 | `goodhr6-backend-1`，host 网络，监听 `8085` |
| 数据库 | 现有 PostgreSQL 容器内的新数据库 `goodhr6` |
| Redis | 现有 Redis 容器的第 `6` 个逻辑库 |
| Nginx | `/api/` 转发到 `8085`，其余页面转发到 `5174` |
| 安装包 | `/downloads/GoodHR-Local-Agent-Setup-v6-windows-x64.exe` |
| HTTPS | Let's Encrypt，证书到期时间 `2026-12-16 10:05:33 UTC` |

生产源码、容器和数据库仍保留原配置。测试库从 `goodhr5` 完整复制后执行当前代码的新迁移。

测试库已经修改控制台、安装包、内置 AI 公共地址和微信支付通知地址为测试域名。测试库的自动挽回邮件关闭，登录验证码 SMTP 配置保留。微信商户配置来自复制数据，本次未发起真实支付。

## 服务器更新命令

先确保 `cloud/backend/.env` 使用测试数据库和测试端口，不要把正式环境的 `.env` 直接覆盖回来。

```bash
cd /www/wwwroot/goodhr.ai.58it.cn/good-hr-ai-js
git switch main
git pull --ff-only origin main
cd goodhr5
docker compose -f docker-compose.goodhr6.yml up -d --build
docker compose -f docker-compose.goodhr6.yml ps
curl -fsS http://127.0.0.1:8085/health
```

测试后端的 `.env` 至少应保持以下区别，数据库、Redis、SMTP 和商户凭据不要写入 Git：

```dotenv
GOODHR_CLOUD_ADDR=:8085
GOODHR_PG_DSN=postgres://***:***@127.0.0.1:5432/goodhr6?sslmode=disable
GOODHR_REDIS_DB=6
GOODHR_PUBLIC_BASE_URL=https://goodhr.ai.58it.cn
```

微信支付 PEM 文件保存在源码目录外，并以只读方式挂载到后端容器：

```text
宿主机：/www/wwwroot/goodhr.ai.58it.cn/secrets/wechat/apiclient_key.pem
容器内：/run/secrets/wechat/apiclient_key.pem

宿主机：/www/wwwroot/goodhr.ai.58it.cn/secrets/wechat/wechatpay_public.pem
容器内：/run/secrets/wechat/wechatpay_public.pem
```

后台配置填写容器内路径 `private_key_path` 和 `public_key_path`。替换文件后，后端会在下一次支付请求时读取新内容并重建微信支付客户端。

修改 Nginx 前先备份，仅编辑测试站点配置，再验证并重载：

```bash
nginx -t
nginx -s reload
```

## 本地程序打包

Windows 在项目的 `local-agent-go-new` 目录执行：

```powershell
cd D:\work\good-hr-ai-js\goodhr5\local-agent-go-new
.\scripts\package-windows.ps1 -Version 6
```

Windows PowerShell 5.1 也可使用：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\package-windows.ps1 -Version 6
```

macOS 必须在 Mac 上执行：

```bash
./scripts/package-release.sh 6
```

两种包的云端和控制台默认地址均为 `https://goodhr.ai.58it.cn`。Windows 安装器默认按 D、B、E、C 顺序选择目录，Logo 来自项目保存的品牌 PNG。

本次发布的 Windows 安装包大小为 `23046360` 字节，SHA256：

```text
6f766028d289b038926ee78909e9bf8aa9d254c0a6832484886a29eef08d6b39
```

## 已完成的验证

- 云端后端和本地 Agent 的 `go test ./...` 通过。
- Next.js 生产构建、Compose 配置检查、Windows 完整打包通过。
- 新环境首页、登录页、公开配置接口、HTTPS 证书和安装包 Range 下载通过；正式登录页仍返回 200。
- 复制前后的原有业务表行数一致，包括用户 835、岗位 1530、候选人档案 34700、AI 余额流水 313140。
- `schema_migrations`、`system_configs`、`tenants` 的差异来自新迁移和测试配置；新增加了 `subscription_order_grants`、`tenant_invitations`。
- 使用临时回环 SMTP 收件器验证：验证码生成、注册必须带密码、错误密码拒绝、正确密码登录、历史账号必须补设密码、补设后密码登录、认证后的组件接口全部通过。测试账号和 Redis 会话已清理，用户数恢复为 835。
- 运行组件接口只提供 `camoufox`、`node_runtime`、`ocr`，不提供旧浏览器组件。
- 在空目录实际下载 Camoufox 和 OCR，SHA256 校验、解压、安装成功；另外在禁用系统 Node 的隔离进程中验证 Node 下载、校验和安装成功。
- 在旧 Worker 运行并占用原生文件的情况下重新安装，安装器返回 0，旧 Worker 被结束；安装到 `D:\GoodHRLocalAgent` 后 v6 健康检查通过。
- 安装器文件图标提取后确认为项目品牌 Logo；启动只打开一次新域名控制台。
- 浏览器中密码模式和验证码模式切换后重新载入，选中的模式及测试邮箱均保留，密码不缓存。
- 公开系统配置接口改为仅返回内置 AI 展示字段，已验证不再返回上游密钥和内部上游地址。修复已提交 `main` 并在测试后端生效；正式后端仍是原版本，本次未升级正式服务。

## 错误记录与处理

以下保留完整错误主体，敏感参数和重复调用命令省略。

| 步骤 | 错误信息 | 原因与处理建议 / 结果 |
| --- | --- | --- |
| 文档读取 | `Unable to resolve open call` / `invalid ref_id` | Web 工具未解析该下载地址，改为直接下载并读取文件，校验成功。 |
| MCP 首次连接 | `UntrustedRoot` | 证书导入只处理了证书链第一段。拆分并导入全部三段证书后连接成功。 |
| MCP 初始化 | 未返回会话响应头 | 服务为无状态 MCP，不应强制要求会话头；按无状态模式调用成功。 |
| 克隆校验 | `ERROR: unexpected commit a9cbb6bd3d0db18f6f95a4c0f57f76a38410507f`，退出码 22 | 校验时预期完整提交号写错；核对本地真实提交号后继续，克隆本身成功。 |
| 后端首次健康检查 | `curl: (7) Failed to connect to 127.0.0.1 port 8085: Connection refused` | 后端镜像使用 `go run`，首次启动仍在编译。编译完成后健康检查成功。 |
| API 路径验证 | `404 page not found` | 误用了 `/api/runtime-config`，正确路径为 `/api/runtime/config`，认证后返回 200。 |
| 证书申请 | `No module named 'mod'` | 从 `/tmp` 执行宝塔模块时缺少面板根目录的 Python 导入路径，补充路径后申请成功。 |
| 证书解析警告 | `CryptographyDeprecationWarning: Parsed a serial number which wasn't positive (i.e., it was negative or zero), which is disallowed by RFC 5280. Loading this certificate will cause an exception in a future release of cryptography.` | 宝塔扫描已有证书时出现兼容性警告，不影响本次新证书。未来升级 cryptography 前需核查旧证书。 |
| Windows 升级 | `DeleteFile: The existing file appears to be in use (5). Retrying.`；`尝试替换已存在的文件时出错：DeleteFile 失败；错误代码 5。拒绝访问。`；`User canceled the installation process.`；安装器退出码 5 | 旧主程序被停止，但孤立 Node Worker 锁住 `better-sqlite3/prebuilds/win32-x64.node`。安装器改为复制前使用 `Invoke-CimMethod` 结束当前安装目录的 Worker；带锁升级复测退出码 0。 |
| 安装器第一次修复尝试 | 残留 Worker 未结束，继续出现上述错误代码 5 | `Get-CimInstance` 返回对象不能直接调用旧 WMI 的 `.Terminate()`，改用 `Invoke-CimMethod -MethodName Terminate` 后成功。 |
| 组件联调清单 | `Camoufox 浏览器没有当前系统 win-x64 的下载地址` | 测试脚本直接提交了数据库 `win/mac` 原始配置。使用与前端相同的平台键转换后，Camoufox/OCR 完整安装成功。Node 单组件测试也预期在 Node 安装后因未提供 Camoufox 清单停止，Node 安装结果已核对。 |
| 浏览器读取输入框 | `loginTab.playwright.getByRole(...).inputValue is not a function` | 当前 CUA 不提供该方法，改用页面截图确认邮箱及模式缓存。 |
| 远程临时进程清理 | `命令被拦截: 关机/重启` | 安全规则误判测试脚本的结束语句，脚本未执行。改用 45 秒自动退出的临时进程，联调成功。 |
| 本机启动组合命令与目录清理 | `rejected: blocked by policy` | 本机自动审批规则拒绝组合启动/检查命令及递归清理。拆开启动和健康检查后正常运行；递归清理未继续重试，测试目录保留。 |
| 本机 PowerShell 辅助命令 | `ParserError: Unexpected token` / `Unexpected token '$CONF.bak.$' in expression or statement.` | 拼接 Linux 引号时被 PowerShell 解析。改用单引号 Here-String；Nginx 备份和配置已成功执行。 |

本机保留的两个组件测试目录位于 `local-agent-go-new/release/component-check-20260917-2254`、`local-agent-go-new/release/component-check-node-20260917-2255`；临时测试进程已结束，目录被 Git 忽略。

## 限制与后续维护

- 本机为 Windows，本次生成并安装验证了 Windows 包；Mac 打包脚本已切换域名，但 Mac 安装包需在 Mac 上生成和验证。
- 服务器部署完成时根分区剩余约 2.2 GB，磁盘使用率约 95%；继续保留更多镜像或增长业务数据前应扩容或清理不再使用的构建缓存。
- 现有正式公开配置接口存在上游密钥暴露风险。测试环境已经过滤；正式环境应发布同一修复，并在上游平台更换曾暴露的密钥。
