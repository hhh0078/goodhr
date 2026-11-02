<!-- 文件作用：记录 Mac 6.2.12.3 暂停自动回复穿插检查的源码、安装包审计、双架构验证和发布状态。 -->

# Mac 6.2.12.3 发布验收

构建日期：2026-11-02。源码为 `eefae920ca5d2a40d0448724ae772450946efef6`，构建前工作树干净。
使用既有 `scripts/package-release.sh 6.2.12.3 build`，npm、Go 均使用原脚本的国内镜像。

## 本次变更的准确范围

- `internal/flow/lifecycle/runner.go` 已注释 `r.runGreetingWithAutoReply(...)`，`greeting` 分支直接调用 `r.greeting.Run(...)`。
- 暂停的是打招呼任务中穿插的自动回复检查，避免尚未上线的检查影响正常招聘任务。
- 独立 `auto_reply` 分支、手动自动回复 HTTP 接口及对应代码仍保留，不能表述为所有自动回复能力均已移除。
- 本次只发布 Mac 通用包，为可忽略更新；Windows 安装包未构建或发布。

## 固定安装产物

| 产物 | 大小（字节） | SHA-256 |
| --- | ---: | --- |
| [Mac universal PKG](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.3-mac-universal-43fdea18.pkg) | 59,307,006 | `43fdea187b7d9ea5e2161a6d7dda0c8b21053e24b3b4dcccf6f0a84e1ad088b9` |
| [Mac universal ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.3-darwin-universal-5836d0d6.zip) | 61,143,800 | `5836d0d6c5c646f9598e5ad01efd3418920c7657642d4245b4f6ed99202671f7` |

本地产物位于 `release/`，使用摘要前八位的固定新文件名，不能覆盖旧版本。更新记录的地址及摘要须对应 PKG 本身。

## 审计和隔离验证

- PKG 展开载荷、打包 App 与 ZIP 的文件内容、符号链接及权限共 2,240 项完全一致；安装前后脚本与源码一致。
- `Info.plist` 两个版本字段和 `build-info.json` 均为 `6.2.12.3`，环境为正式环境，地址为 `https://goodhr.58it.cn`。
- Go 程序包含 arm64、x86_64 切片；从 `eefae920` 按相同正式参数复编，两份切片与包内对应程序逐字节一致。
- 旧 `6.2.12.2` 两切片均存在 `runGreetingWithAutoReply` 函数符号，本次两切片均已不再链接该符号。
- 本次仍存在普通 `greeting.(*Flow).Run` 及独立 `auto_reply.(*Flow).Run` 符号，与源码暂停穿插检查、保留独立分支一致。
- 两切片完整内嵌前程无忧配置及当前书签脚本；前程入口、旧默认入口删除和用户书签保留规则仍在包内。
- Apple Silicon 原生及 Intel/Rosetta 两套最终 PKG 主程序在独立数据目录、端口运行，`/health` 均返回 `6.2.12.3`。
- 两架构对应的 Node 22.19.0 均能加载包内 `impit` 原生模块；最终包 Worker、Go→Worker 健康桥和选择器数组解析通过。
- Worker 包版本为 `0.1.0`、健康协议为 `v1`，与应用发布版本用途不同，未用协议号冒充新应用版本。
- 最终主程序生成的隔离书签资源执行两项回归通过；未绑定时原有启动门闩仍阻止浏览器启动。
- 主任务补充回归结果：37 通过、1 跳过；Windows amd64 交叉编译通过，未生成 Windows 安装包。
- 没有使用招聘页面、真实账号或 Profile，没有请求正式端口 `43129`、`39881`，没有发送招聘消息。
- 所有自建 Agent、Worker 均正常退出，退出码为 0；正式 App、旧安装包和历史证据保持。

## 验证边界和证据

PKG 仍为 `Status: no signature`，没有新增签名、公证或实际系统授权安装验证。Intel 为 Rosetta 验证，不冒充实体 Intel Mac。
正常 PKG 升级沿用旧 Go 先关浏览器、再停自有 Worker 的收尾路径；新程序随后加载新 App 内 Worker。
未新增 Worker 哈希握手。异常退出遗留的孤儿 Worker 不能称为自动解决；应完整退出旧实例再重开，仍有残留时重启电脑后再试。

构建日志：`/tmp/goodhr-mac-62123-build.log`。
包内容、双切片复编及符号证据：`/tmp/goodhr-mac-62123-audit/artifact-audit.json`。
双架构健康、书签及进程清理证据：同目录 `runtime-audit.json`、`runtime-arm64/`、`runtime-x64/`。

## 发布状态

已发布 Mac `6.2.12.3` 可选更新。两个公开地址均完整读取并核对 HTTP 200、字节数及 SHA-256；更新记录的地址和摘要对应本表 PKG。

- 仅前置一条 Mac 记录，总数 57 → 58；原 57 条记录的内容和顺序完整保留，其它 onboarding 字段保持原样。
- Windows 最新仍为 `6.2.12.3`，整条记录与发布前一致，本次没有重新打包 Windows。
- 使用实际前端函数确认 `6.2.12.0/.1/.2 → 6.2.12.3` 为可忽略补丁，岗位启动不被本次更新拦截；Windows 选中的完整版本信息不变。
- 公开更新接口和下载页均 HTTP 200，下载页已显示本次 Mac PKG 与原 Windows 地址。
- 原前后端、PostgreSQL、Redis 的容器 ID、镜像及启动时间保持一致，内部健康检查通过；没有云端重新部署或数据库迁移。
- 发布后服务器根分区余量约 6.19 GiB，下载站点分区约 4.61 GiB。只移除了本次两个已验证上传临时文件，没有清理旧包、历史备份或其它服务。

本机发布证据：`/tmp/goodhr-mac-next-release-20261102/` 下的 `public-artifacts-verified.json`、`public-release-verification.json`、`update-preview-proof.json` 和 `server-post-release-proof.json`。
服务器证据及更新前后私密配置备份：`/opt/goodhr-release/mac-6.2.12.3-20261102/`，包括 `baseline.json`、`published-artifacts.json`、`mac-update-publish-proof.json`、`post-release-verification.json`。
