<!-- 文件作用：记录 WorkBuddy 任务通道地址、会话重连、脱敏诊断与双平台验证要求。 -->

# WorkBuddy 本地任务通道修复

## 故障与职责

WorkBuddy 连接器通过云端 MCP 下发任务，实际运行仍由本地 Go、Worker 和招聘浏览器完成。本轮不修改 WorkBuddy 客户端、市场连接器或其已安装文件。

首次实测确认两个独立故障：旧客户端把 HTTPS 地址直接交给只接受 `ws/wss` 的 WebSocket 客户端；改正后，正式 API 所经的静态 CDN 又移除了 Upgrade 请求头，源站明确记录握手失败。源站 Nginx 的 `/api/` 已正确转发至正式 8086，`goodhr5.58it.cn` 是已有的同后端直连域名。

## 地址配置

- `config.Config.CloudURL` 继续用于云端 API，控制台地址也保持原值。
- 新增 `Config.CloudWSURL`，只交给 `internal/cloudws` 命令通道客户端。
- 可用 `GOODHR_CLOUD_WS_BASE` 显式配置任务通道，或编译时通过 `-X goodhr5/local-agent-go-new/internal/config.DefaultCloudWSURL=...` 指定默认值；默认值为空时使用当前 API 地址。
- 官方打包且 API 为 `https://goodhr.58it.cn` 时，任务通道默认使用 `https://goodhr5.58it.cn`。转换为 WSS 后正常校验证书，不固定 IP、不关闭 CDN 或 TLS 检查。
- 私有或测试打包沿用自身 API 地址。官方包在运行时覆盖 `GOODHR_CLOUD_API_BASE` 为其他地址、且未显式指定任务通道时，也跟随该私有地址，不连接官方任务通道。
- 构建记录 `build-info.json` 增加 `cloud_ws_base_url`，方便核对实际候选配置。

## 会话与错误处理

`internal/cloudws/cloudws.go` 统一将 HTTP/HTTPS 转为 WS/WSS，使用 Bearer 请求头认证，凭据不放进 URL。连续失败按 5、10、20、40、80、120 秒等待，严格不超过两分钟；曾成功握手后断线恢复五秒重试。既有凭据读取入口每五秒检查更新，等待或已连接时换号都能结束旧通道并重新认证，不新增账号同步 API。

本机 `GET /api/v1/cloud/session` 保留凭据存在性与在线状态，新增 `ws_error_code`、`ws_http_status`。只返回固定错误分类和 HTTP 状态，不返回令牌、地址或服务器响应正文。成功连接会清除旧错误。

## 平台实现与验证

Windows x64、macOS Apple Silicon 和 Intel 共用 Go 连接与配置逻辑，无系统专属网络调用。Windows 打包入口为 `scripts/package-windows.ps1`，Mac 为 `scripts/package-release.sh`，两者均写入独立任务通道默认值，保留原 API 和控制台域名。Mac 通用 PKG 继续执行固定 `/Applications` 的安装位置检查。

源级回归包括 HTTP/HTTPS 地址转换、真实 WebSocket Bearer 握手和岗位回执、失败响应脱敏、成功清错、退避封顶及恢复、新凭据在退避和已连接场景下的五秒内切换，以及开发、官方、私有配置隔离。Go 编译须覆盖 Windows amd64、macOS arm64 和 macOS amd64。

三目标编译不能替代真实安装和线上 WSS 验收。Mac 正式完整包与真机结果记录在本轮 WorkBuddy 验收文档；Windows 和 Intel Mac 的实际安装、网络环境仍需对应系统复测。Windows PowerShell 打包脚本在 Mac 上只做静态核对，不能冒充 Windows 打包运行已通过。
