<!-- 文件作用：说明本地程序的云端会话跨重启恢复、退出边界与 Windows/macOS 验证方式。 -->

# 本地程序云端会话恢复

本地程序接收控制台或连接器授权页的已验证登录会话后，重启无需再次打开浏览器登录。沿用云端原有 30 天会话有效期，不延长、不自动刷新，也不保存密码。到期或被服务器明确拒绝后，需要重新登录连接。

## 唯一保存与恢复链路

- `POST /api/v1/session/bind` 和 `POST /api/v1/cloud/session` 先走现有云端校验，再经同一个 `saveCloudSessionLocked` 保存，命令通道和现有本地运行账号一同切换。受限浏览器插件会话不允许保存为完整本地会话。
- 凭证保存在 `cfg.DataDir/cloud-session/session.json`，仅含规范化的 `cloud_url`、`ws_url` 和 `token`。不进入 SQLite、日志、诊断 GET、安装包或 WorkBuddy 源码。
- API 与 WS 基址同时匹配才恢复；不同协议、主机、非默认端口、路径前缀或 WS 目标不能借用原会话。协议/主机大小写、默认端口和末尾斜杠正常归一化。
- `NewServer` 只读本地文件，断网不会阻塞本地 HTTP 启动。现有 WS 循环用恢复凭证向服务器认证并重试；握手成功后最多 8 秒验证现有 API 会话，恢复运行账号及现有浏览器组件检查。取消程序运行会取消该请求。
- WS 和每次任务启动前检查继续由服务器鉴权，文件里有凭证不等于任务获准执行。网络中断、超时、403、5xx 保留本地记录；明确 401 清理当前对应凭证，不把暂时故障当成用户退出。403 可能来自代理或权限策略，不能统一当作会话到期。
- 如果命令通道已经在线，而之前 API 暂不可用导致浏览器检查未完成，下一次有效岗位命令会重试同一组件检查。门闩未通过时明确回复“正在检查浏览器组件，请稍后再试”，不接受必然失败的任务；检查通过后下次命令继续原任务入口，不新增定时器或等待流程。

## 退出与并发

网页退出继续使用 `POST /api/v1/session/unbind`。匹配当前身份时同时删除私有文件、内存云端凭证和现有运行绑定；现有 WS 账号检查在最多 5 秒内断开旧连接。删除失败必须报告失败，不能假装退出已完整保存。

保存复用现有 `extensionMu` 锁，以请求代际和待确认凭证防止迟到校验恢复已退出或被替换的身份；原有 `extensionGeneration` 继续负责已发布账号的运行与迁移。B 仍在确认时退出只取消 B；旧 A 退出不取消已经在确认的 B。旧账号解绑或旧连接的 401 不得删除新账号。会话写入/清理入口显式拒绝不允许的 `Origin`，不只依赖 CORS 响应头。

插件配对确认和下载导入设置刷新原本也会更新完整网页登录身份，因此仅将其已经校验过的普通网页凭证接到同一保存入口。插件收到的仍是独立受限会话，不把插件凭证保存为 WS 登录态，不改变插件交换和下载业务。

## 平台差异

- macOS Apple Silicon 与 Intel 共用 `permissions_unix.go`：独立目录 0700、文件 0600。默认位置为 `~/Library/Application Support/GoodHR/local-agent-new/cloud-session/session.json`。
- Windows x64 使用 `permissions_windows.go` 和项目已有 `golang.org/x/sys/windows`，设置当前用户 SID 的独占 DACL，取消来自父目录的权限继承。Windows 的 `0600` 不是访问控制依据。位置仍由现有 `cfg.DataDir` 决定，不改变安装器或其他业务目录。
- 两端均拒绝符号链接和非普通文件，限制记录大小。同目录临时文件写入并 `Sync` 后通过 `os.Rename` 完整替换，失败不会先删除旧文件。Go 在 Windows 使用 `MoveFileEx(MOVEFILE_REPLACE_EXISTING)`；Windows 不宣称具备 Unix 文件系统的全部原子和断电语义。替换失败会明确报错、保留原文件，不使用删除后重建的降级。
- 使用本机用户权限保护文件，不引入 Keychain、DPAPI 或额外依赖。操作系统管理员仍具有机器管理权限。

## 回归方式

`internal/system/sessionfile/session_test.go` 验证真实文件更新、权限、双目标隔离、损坏/超限/链接拒绝与删除。`internal/api/cloud_session_restore_test.go` 使用隔离数据根和真实本机 HTTP/WS，验证保存后新建服务自动认证、无浏览器恢复、暂时故障保留、401 清理、换号并发与恶意来源拒绝。`internal/cloudws/cloudws_test.go` 验证真实 WS 401 回调与 403/5xx 保留。

定向运行 `go test ./internal/api ./internal/cloudws ./internal/system/sessionfile ./internal/bootstrap`，对上述安全并发路径运行 `go test -race`。主程序分别编译 `windows/amd64`、`darwin/arm64` 和 `darwin/amd64`。Mac 文件/HTTP/WS 回归在本机运行；Windows DACL 和实际安装后恢复仍需 Windows 真机验收，交叉编译不能替代该项。

2026-11-26 本轮验证：上述四包普通测试与 `-race` 全部通过，三个主程序目标编译通过，`git diff --check` 通过。使用隔离的合成账号与数据根，未修改本机真实账号凭证或安装中的应用。
