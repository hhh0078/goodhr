<!-- 文件作用：记录 WorkBuddy 新用户安装、账号绑定及任务连接验收，明确真实结果与未覆盖范围。 -->

# WorkBuddy 首次安装验收

验收日期：2026-11-26（北京时间）。目标是只有 WorkBuddy、尚未安装 GoodHR 的用户可以完成账号连接、安装、组件准备和后续任务使用。用户确认只实测 Mac；Windows 同类代码一并修复，由用户额外打包实测。本轮最终不修改 WorkBuddy 客户端、市场连接器或其已安装代码，市场连接器仍为 1.0.3。

## 当前真实结果

- 6.3.4.13 PKG 已于 08:06:08 完成系统安装，`/var/log/install.log` 有明确的 `Installed` 记录；原“等待系统认证”状态已经结束。此前 02:13 的 WorkBuddy `check_setup` 已返回 `online=true`、`ready=true`、版本 `6.3.4.13`，历史截图 `workbuddy-ready-6.3.4.13.png` 保留。
- 12:38，WorkBuddy 5.11.3 的真实新对话《检查 GoodHR 助手当前状态》实际调用 `check_setup` 与 `get_quota`，返回 `online=true`、`ready=true`、版本 `6.3.4.14`，并读取到 14 个岗位。这次调用早于下面的 .14 PKG 安装完成时间，不能改写为安装后的复查。
- 6.3.4.14 正式 PKG 于 12:46:54 完成系统安装，`install.log` 明确记录 `Installed "GoodHR-Local-Agent-Setup-v6.3.4.14-mac-universal-15c8c0a1"`。12:50:37 的 `mac14-installed-live-state.json` 确认实际运行的是 `/Applications/GoodHR Local Agent.app` 中的 launcher（PID 40963）与主程序（PID 41009）；本机版本为 .14，`has_token=true`、`ws_connected=true`，错误分类为空。
- 同一正式安装后的状态确认 `node_installed=true`、`node_ready=true`、`camoufox_installed=true`、`worker_built=true`、`node_worker_installed=true`。当时 `worker_ready=false`，表示尚未启动按需 Worker；打开页面或执行任务前由现有 `EnsureWorker` 链路启动，不能把这一项单独解释为程序离线。
- 12:56，正式安装后的 WorkBuddy 再次实际调用 `check_setup` 与 `get_quota`，返回 `online=true`、`ready=true`、版本 `6.3.4.14`、14 个岗位，`last_seen_at=2026-11-26T04:47:00Z`（北京时间 12:47，位于正式安装之后）。证据已写入 `current-acceptance-status-14.json` 的 `workbuddy_14_after_install_evidence`。
- .14 公网完整下载、官网配置切换及公开页面复核均已完成，时间、摘要及平台选择结果见下面的发布记录；本轮安装与连接器只读使用链路已完成验收。

旧 WorkBuddy 对话没有加载连接器工具，只能说明该对话的工具未就绪，不能据此判定本地程序离线；实际状态以有工具的新对话调用和本机/云端状态为准。本轮已验证账号连接、系统安装、运行组件、云端任务通道及只读岗位查询，没有启动真实招聘任务或发送招聘消息，不能据此宣称招聘平台已登录或真实筛选已经完成。

## 干净设备场景与安装问题

- 最初停止旧 GoodHR、Go 与 Worker，旧 App 移至废纸篓；未清空废纸篓。
- 两个原数据目录完整备份至验收目录 `backup/GoodHR` 与 `backup/GoodHRLocalAgent`，新目录从空状态初始化，未复用旧 Profile、Node 或浏览器。
- 通过本人账号管理入口解除该账号的所有历史设备绑定。保留原账号、岗位和简历，不能称为新建了一个空云端账号；零岗位另有合成回归。
- 官网原 6.3.4.11 包 SHA256 一致，但两次真实安装失败。`install.log` 明确显示 PackageKit 将 App 重定位到源码 release 目录，导致 `/Applications` 下没有程序，postinstall Code 112。
- 已通过 component plist 禁止重定位，检查实际 PKG 的 relocate 项，并改善目标程序缺失时的错误。修复包装的 .11 于 01:14 真实安装成功；完整 .12 于 01:33 真实安装成功。
- .13 与 .14 后续正式 PKG 分别于 08:06:08、12:46:54 安装成功；.14 的静态产物审计与真实系统安装分别留证，不能用前者代替后者。
- 空数据目录首次下载 Camoufox 成功；现有“安装必要组件”按钮真实下载安装 Node 22.19.0 成功。新 Profile 浏览器打开 Bing 及 BOSS 页面；招聘平台账号登录未完成。
- 已修复界面把单独浏览器修补进度 100% 显示为全部组件完成的问题；缺少 Node 时继续明确显示缺失与安装入口，等待函数不能误报成功。

## 任务连接根因与修复

1. .11 把 HTTPS 直接交给仅接受 WS/WSS 的客户端。改为协议转换，并使用 Bearer 请求头，凭据不再进入 URL。
2. .12 仍不能握手。02:01 在正式后端读取本账号日志，明确报 `websocket token not found in Upgrade header`；实际 Nginx `/api/` 已正确代理到 8086 并转发 Upgrade，CDN 链路移除了该头。
3. 官方打包仅把任务通道指向既有同后端直连域名 `goodhr5.58it.cn`，API 和控制台仍为 `goodhr.58it.cn`。正常校验 TLS，不固定 IP、不修改全站 CDN；私有/测试环境保持各自地址。双域来源也由部署文档、发布健康检查和实际两套 Nginx 配置确认。
4. 退避上限由实际 160 秒修到 120 秒，成功连接后恢复 5 秒；换凭据在 5 秒内重新认证。增加不暴露凭据、URL 或响应原文的错误分类和 HTTP 状态。

实现及平台说明：`local-agent-go-new/docs/workbuddy-command-channel-20261126.md`。相关 Go 包、真实合成 WS Bearer 握手和任务回执、连接换号及 race 测试均通过；Windows amd64、macOS arm64/amd64 均用正式域名参数实际编译通过。Windows PowerShell 脚本保留 BOM/CRLF，尚未在 Windows 打包或运行。

## 账号选择、注册与授权

- 无论浏览器是否已有会话，绑定页始终先让本人选择“我有账号 / 我还没有账号”，不自动使用缓存账号。
- 已有账号复用密码、验证码和微信登录；没有账号复用现有邮箱验证码、密码确认和协议注册流程。登录或注册成功后自动继续 OAuth。
- 注册请求使用 `register_only:true`。已有邮箱（包括无密码老账号）明确返回 409，不自动改走登录；原子创建防止并发覆盖密码。邀请码、试用、协议和原登录流程保留。
- 服务端先核验已注册客户端、精确回调和完整 S256；授权页显示客户端自报名称、回调来源与完整地址，并要求独立的未默认勾选权限确认。危险回调协议、历史危险客户端以及缺失明确同意决定均被拒绝。
- 本轮认证身份固定；其他标签换号时重新选择，不能替新账号授权或设置密码。已安装程序先有界完成绑定、会话同步，再继续 OAuth；未安装不阻断账号授权。过期登录在当前页面重选，OAuth 失败只重试绑定。
- 原有“可随时在控制台解除”承诺因没有对应功能而移除，未新增 OAuth 授权撤销接口。下面的普通网页登录注销仅撤销本次网页登录会话，不等同于解除 WorkBuddy OAuth 授权。

## .14 重启恢复、退出与按需 Worker

- 已校验的普通网页登录会话通过同一保存入口写入当前用户私有文件，API 与 WS 目标同时匹配才恢复；沿用服务端原有 30 天期限，不自动续期，不保存密码，不写入 SQLite、日志或安装包。Mac 使用 0700/0600，Windows 使用当前用户 SID 的独占 DACL；具体边界与平台说明见 `local-agent-go-new/docs/cloud-session-restore.md`。
- 独立 .14 进程使用隔离端口 43130、`auto_open_console=false`，不依赖浏览器打开控制台即恢复 `has_token=true`、`ws_connected=true`，退出码 0，`session_file_unchanged=true`。证据 `mac14-headless-restart.json` 只记录状态与文件未变结果，不展开原始凭据；该项与随后正式 PKG 实装分别验收。
- 网页显式退出先通过 `POST /api/auth/logout` 原子撤销本次普通网页凭证，再尝试本地 `/api/v1/session/unbind`，最后清理浏览器。云端故障保留登录状态供重试，不假装退出成功；等待期间换号不清除新账号。即使本地程序已经关闭，原凭证也不能在下次启动时恢复授权，明确 401 会清理对应本地记录。
- 本地绑定、恢复、换号、退出与 401 清理复用同一身份规则；B 仍在确认时退出只能取消 B，旧 A 的延迟退出或 401 不得取消在途 B。临时断网、超时、403 或 5xx 保留记录，不冒充明确注销。
- Worker 和招聘浏览器按需启动。WS 已在线但之前 API 暂不可用导致浏览器检查未完成时，新的有效岗位命令会重试现有组件检查；门闩未通过先明确返回“正在检查浏览器组件，请稍后再试”，不接受必然失败的任务。没有为了显示 `worker_ready=true` 而提前启动招聘浏览器或真实任务。
- 上述修复均在 GoodHR 云端、前端或本地程序完成，没有修改 WorkBuddy 客户端、市场包或已安装连接器；没有关闭 TLS 验证或修改全站 CDN。

## 已执行回归

- 后端注册定向、race 与完整 HTTP API 回归通过；真实隔离 PostgreSQL 验证重复/老账号保护及 16 路并发单赢家通过。测试库位于临时容器 tmpfs，容器已精确停止移除，未操作业务库。
- OAuth 真实路由回归覆盖动态注册、公开预检、授权同意/拒绝、PKCE 交换、MCP、refresh 轮换及危险历史回调，定向、race、全 HTTP API 通过。
- 授权前端功能分支 29 项浏览器回归、10 项共享 API 单测、TypeScript 检查通过；1366px / 390px 截图已人工查看。所有认证、邮件和组件接口均为合成数据，未创建真实新账号或发送注册验证码邮件。
- 合并 main 时保留其既有微信协议前置与登录偏好逻辑；main 后端完整 HTTP API 通过（73.102 秒）。main 前端 32 项集成浏览器回归、10 项 API 单测、TypeScript 检查全部通过，覆盖 main 既有微信协议前置、原登录缓存及授权页忽略旧偏好；对应桌面/手机截图位于 `oauth-account-flow-main/`。
- .14 会话文件、API、WS 与 bootstrap 四包定向及 race 回归通过，覆盖重启恢复、目标隔离、真实文件权限、写入失败、401/临时故障、换号与在途退出；Windows amd64、macOS arm64/amd64 主程序编译通过。WS 触发组件检查重试另有回归，不请求真实招聘任务。
- 普通网页注销复用现有会话存储的原子撤销；真实隔离 Redis 双客户端、Memory 与认证/密码/插件相关 race 回归通过（11.281 秒），前端 11 项 API 单测、5 项桌面/手机注销回归及 TypeScript 检查通过。测试覆盖本地关闭、退出期间断开、云端失败重试与等待期间切号；已更新的教程完整链路退出用例未在这一轮重新运行。
- 组件界面 5 项单测、4 项桌面/手机回归与类型检查通过；下载记录和零岗位引导的后端回归通过。
- Mac 单独新增发布记录只填写 `url_mac`、PKG 的 SHA256，`url_win` 留空且不填通用 `url`。此前真实前端选择函数已验证独立 Mac 记录不使 Windows 误报升级；本次 .14 实际切换保留原 Windows .11 地址与摘要，见发布记录。

## 固定 Mac 产物

当前 .14 业务源码为 main `bb39c4c2b73241cc20d2ddb32cbd453fff78b6ff`，使用标准 `zsh scripts/package-release.sh 6.3.4.14 build`。先核对 npm 与 Go 国内镜像，未新增依赖。

- PKG：`GoodHR-Local-Agent-Setup-v6.3.4.14-mac-universal-15c8c0a1.pkg`
- 大小：`60,914,609` 字节。
- SHA256：`15c8c0a139d32b375c97d4e59b2c8f8bb33bf5ae68b547a755af831205f4f079`。
- 同一 App、ZIP、PKG 均由本轮标准脚本生成；Mac Go 与 launcher 均有 Apple / Intel 切片，运行依赖按双架构锁文件准备。实际 PKG 固定 `/Applications` 校验通过。
- `mac14-artifact-audit/result.json` 确认固定安装路径为 `/Applications/GoodHR Local Agent.app`、relocate bundle 数为 0、主程序与 launcher 均含 x86_64/arm64。该静态审计本身不宣称完成系统安装；真实安装依据为上面的 `install.log` 与 `mac14-installed-live-state.json`。
- 包仍未签名、公证，不能将已有“仍要打开”引导称为已解决签名问题。

历史 .13 包 `GoodHR-Local-Agent-Setup-v6.3.4.13-mac-universal-12ddbd13.pkg`、SHA256 `12ddbd13832a1d3d334f06f066fd773188bd3f3ce980f1c6f034483d59a1fcf1` 与当时任务连接提交 `8f6457aa` 保留追溯。本机证据与产物统一在 `/Users/Zhuanz/Downloads/goodhr-workbuddy-acceptance-20261126/`，原备份保留。

## 发布与验收范围

- 账号选择、严格注册和普通网页登录注销已随 release `auto-bb39c4c2b732-20261126T005613Z` 上线。公开无鉴权 `POST /api/auth/logout` 已返回 401，授权元数据 `GET /api/workbuddy/authorize` 无有效客户端时返回 400；实际后台及授权页引用的公开 JS 含 `/api/auth/logout`、账号选择文案与 `register_only`，均读取成功。证据 `public-cloud-features-20261126T032444Z.json`；这项只证明公开接口与前端代码已发布，不冒充一次真实新账号注册。
- 最终服务器记录 `mac14-server-final-state.json` 确认 release `auto-bb39c4c2b732-20261126T005613Z` 为“已上线”，`source_commit` 与 `deployed_commit` 均为 `bb39c4c2b73241cc20d2ddb32cbd453fff78b6ff`；本轮隔离证据的 `success`、`legacy_data_preserved`、`rollback_verified`、`schema_verified` 全为 true。
- 上传后的最终资源记录为 15 个运行容器、22 个镜像，与上传前一致；根分区可用 `15,829,516,288` 字节，`/www` 可用 `3,202,625,536` 字节。后者比 3GiB 构建预算少 `18,599,936` 字节，后续涉及实际构建或激活仍须按现有空间门禁检查；未运行全局 prune、删除原始备份或改动 CDN。
- 随后通过宝塔 MCP 复核实际 DockerRoot 为 `/www/docker/engine`，可用 `3,204,120,576` 字节，约 2.98GiB，仍低于 3GiB；下次云端代码发布前须按现有规则恢复预算。本次仅将验收文档变更提交到 main，依据已上线源码 `bb39c4c2` 的 `scripts/auto_release.py`，`docs` 不属于 `CLOUD_PATHS`，会健康检查后跳过构建，不进入构建空间预检，不影响已上线的 bb39 版本，无须为本次文档提交清理空间。
- .14 已通过宝塔 MCP Upload 完成上传，`ready=true`，大小及 SHA256 匹配，随后在 oss2 站点目录发布为 0644，未覆盖既有同名文件。公开地址为 `https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.4.14-mac-universal-15c8c0a1.pkg`。
- 12:53:47 从公开地址完整 GET 下载，HTTP 200，实际取得 `60,914,609` 字节，全文 SHA256 与上面的固定产物一致，`expected_sha256_matched=true`；证据 `mac14-public-full-download.json`，不是仅凭 HEAD 或上传成功判断可下载。
- 12:54:44 通过正式后台接口 PUT 保存下载配置成功，HTTP 200。Mac .14 记录置于 `local_agent` 首位，旧 20 项逐项原样保留；`runtime_components` 和其他所有字段不变。Windows 仍使用 .11 的 `https://oss2.58it.cn/goodhr-local-agent-v6.3.4.11-windows-x64-e3eee3cb.zip`，SHA256 `e3eee3cb4e78e1ac84a076cf25e90e6f0af8ac96224ef84cd2d41c84b01b6d90`。
- 配置切换证据为 `mac14-download-cutover-result.json`，前后完整备份为 `onboarding-before-mac14.json`、`onboarding-after-mac14.json`；原有历史版本与回退材料未删除。此前 11:24 公开快照仍显示 .11，是本次切换前的历史状态，不能继续作为当前下载入口结论。
- 12:55:26 再读公开 `/api/system/local-agent-updates`，HTTP 200，首个 Mac 包已为 .14、首个 Windows 包仍为 .11；公开 `/download` 同为 200，HTML 中两端下载链接均正确。证据 `mac14-public-cutover-verification.json`。
- 本轮安装、重启恢复、正式包公网交付和真实 WorkBuddy 连接器只读调用已完成。未覆盖招聘平台账号登录、真实筛选/打招呼、结果回取，以及 Windows 与实体 Intel Mac 的实机安装和运行，不将编译或只读查询扩大为这些场景的验收结论。
