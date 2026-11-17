<!-- 文件作用说明：记录 Mac 6.3.4.5 候选人加载修复的固定源码、可选更新发布、完整性验证与条件回退依据。 -->

# Mac 6.3.4.5 发布记录

2026-11-17，按用户确认发布 Mac 非强制更新。源码为已推送到 Gitee/GitHub `dev` 的 `cd6f0377d10a78a75b657c6d8b960e611e8b1a89`，使用独立 Git 归档和 `local-agent-go-new/scripts/package-release.sh 6.3.4.5 build` 构建。没有合并或发布云端 `main`，没有包含原工作区未跟踪文件。

## 修复与更新方式

- 保留真实滚轮追赶本批末尾的修复，解决连续跳过候选人后页面落在上方的问题。
- 下一批检测只用已有 `ProbeOnly` 探测一张卡片，完整资料在下一批读取，避免等待新人与重复读整批共用两分钟时限。
- 保留上一版 Mac 已发布的“确认聊天后先发招呼语、再索要资料”修复。
- 使用第四段补丁号；正式更新接口返回的新记录经现有前端版本函数验证，6.3.4 至 6.3.4.4 升至 6.3.4.5 均可忽略，正常余额下允许启动。更早基础版本的既有升级规则不变。

## 固定产物

| 格式 | 字节数 | SHA256 | 下载 |
| --- | ---: | --- | --- |
| PKG | 60865365 | `42cfe54308f109b2c233ca3adc7e9aa7102069cc72bdf36e55e4066cc111d473` | [Mac 安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.4.5-mac-universal-42cfe543.pkg) |
| ZIP | 62706251 | `6118319f7cf8ce6c5d8400e6fa1a5fa6be0661b86ff737512c825920d8d867d7` | [Mac 应用压缩包](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.5-darwin-universal-6118319f.zip) |

App、PKG、ZIP 内 2452 项路径、内容和权限一致，清单 SHA256 为 `0793620402ffd460a41f01fd2f82f59e0c2b53d298c5f948e7dbd5d88f0d40a8`。Go 主程序及启动器均核实 arm64/x86_64；impit 原生依赖分别核实 Apple/Intel 架构。两个 Info.plist 版本字段、build-info.json 与原生 Go 程序 `--help` 均确认为 6.3.4.5，正式地址为 `https://goodhr.58it.cn`。

## 验证范围

Go 平台目录、greeting、browser/client、config、updater、system 和主程序相关测试通过；新增回归覆盖已有新人、滚轮后追加、慢页面只做探测，以及下一批完整资料与游标保留。Worker 编译及探测/滚动测试 25 项、前端启动版本测试 14 项通过。Windows amd64、macOS arm64/amd64 完整程序编译通过。

没有测试账号，未验证真实招聘平台端到端流程或实际发送；没有覆盖安装当前客户端，也没有 Windows/Intel Mac 真机安装运行。安装包沿用未签名、未公证形式，编译和合成测试不能代替上述验证。

## 发布保护与公开结果

服务器 `/opt/goodhr-release/mac-6.3.4.5-20261117/` 保存完整配置前后快照、`update.sql`、`rollback.sql`、事务回滚演练、源码归档、产物、清单、构建日志以及容器和磁盘基线；目录 0700，文件 0600。更新与回退均锁定配置整行并比对预期，发生并发变化即停止。

- 只在 `system.onboarding_config.local_agent` 首位新增一条 Mac 记录，Windows 地址为空；原七条公开记录逐项保留，其他配置与行字段不变，事务更新后回滚演练通过。
- PKG、ZIP 均经公网 HTTPS 完整读取，HTTP 200，长度与 SHA256 与上表一致。更新接口与 `/download` 均为 200，Mac 入口指向本版 PKG；现有正式选择函数确认 Windows 仍为 6.3.4.2。
- 发布前后 15 个运行容器的 ID、镜像 ID、启动时间均相同，镜像列表无变化，没有创建构建或隔离容器、网络。
- 收尾时根盘余量 20137480192 字节（约 18.75 GiB），`/www` 余量 3816435712 字节（约 3.55 GiB）；相比基线分别占用增加约 270 MiB、119 MiB，包含本次安装包、原始归档与证据及同期正常写入。
- 打包 stage 和校验解包目录已自动清理；没有删除历史安装包、配置备份、数据库材料或其他项目数据。

本机证据目录：`/Users/Zhuanz/Documents/Codex/2026-11-17/goodhr-mac-6345/`。
