<!-- 文件作用说明：记录岗位两轮筛选功能 Windows 6.2.11.3 安装包、验证结果、正式更新配置和精确回退材料。 -->
# Windows 6.2.11.3 筛选记录发布

2026-10-26，基于最新主分支提交 `bf01c192929943d64968752854d30875ec566375` 构建并发布 Windows x64 本地程序。本次仅发布已存在的筛选记录功能，没有修改程序源码、云端镜像或数据库结构。

## 安装文件

- 正式构建命令：在 `goodhr5/local-agent-go-new` 执行 `./scripts/package-windows.ps1 -Version 6.2.11.3 build`。
- 下载地址：<https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.11.3-windows-x64-0a1870f0.exe>。
- 文件大小：31,196,595 字节。
- SHA256：`0a1870f0373e31762b5aca57ed717f9955539a349152ba14ea1a6da51a37170f`。
- Setup ProductVersion 为 `6.2.11.3`；包内 `build-info.json` 为 `6.2.11.3 / production / https://goodhr.58it.cn`。
- `go version -m` 确认 Windows amd64、CGO 关闭、源码提交为上述主分支提交、`vcs.modified=false`；构建工具链为 Go 1.27.0。
- npm 使用 `registry.npmmirror.com`，Go 使用 `goproxy.cn,direct`；没有升级依赖版本。

更新后，新筛选开始记录列表预筛和详情评分的分数、原因、起止时间与真实耗时；断网后保留并补传。历史未采集的信息不补造。

## 验证结果

- Go 普通完整回归通过：34 个测试包，501 个顶层用例通过、19 个环境相关跳过；含子用例共 863 个通过、0 个失败。筛选相关七包含子用例 298 个通过、1 个真实滚轮环境相关跳过。
- `go vet ./...` 通过。当前机器没有 gcc 且 CGO 关闭，本轮未执行竞态检测。
- Worker 共 217 项：215 项通过、2 项跳过、0 项失败。
- 包内 Windows EXE 使用独立数据目录与独立 Agent/Worker 端口启动，健康接口返回 `6.2.11.3`；Worker 启动与状态正常；实际 SQLite 已创建 `screening_records` 表。随后关闭本次 Worker 和精确 PID 对应的验证进程。
- macOS arm64、amd64 均完成同一源码交叉编译，元数据确认对应目标和 `vcs.modified=false`。本次没有重新发布 Mac，也没有执行 Mac 真机运行。
- 本机原件、服务器文件与公网完整下载的 EXE 大小、SHA256 三处一致。
- 更新接口返回 Windows `6.2.11.3` 和相应 EXE SHA256；正式下载页 HTTP 200，包含新的 Windows 直链。生产健康接口 HTTP 200。

本轮没有执行用户电脑的完整安装、自动覆盖升级或真实招聘平台操作；为避免安装器终止现用同名程序，独立启动验证直接运行包内 EXE。

## 更新配置与回退

目标主机为 `ldclouda31539`，数据库为 `postgres` 容器中的 `goodhr5`。只在 `system.onboarding_config.local_agent` 最前插入独立 Windows 记录，条数从 51 变为 52。

Mac `6.2.11.3` 原记录及 SHA256 `63bdfe9582b7b79b08fa638066dce0451b61c23dfd64c3579c671f24f57c5793`、全部历史记录和顺序、其他 onboarding 字段、`runtime_components` 以及整行 `system.app_config` 均保持一致；三个旧下载 URL 字段没有修改。Windows `6.2.11.2` 至 `6.2.11.3` 为第四段更新，沿用现有可忽略规则。

发布前保存配置两行完整快照和带原值保护的回滚 SQL。在临时表副本执行同一发布与回滚 SQL，逐行确认恢复原值后回滚临时事务；随后正式事务锁定配置行、核对完整原快照，仅更新 onboarding 一行。事务内外均验证历史、Mac 和其他字段保持。

服务器材料目录：`/opt/goodhr-release/windows-screening-6.2.11.3-bf01c192-20261026/`，权限仅 root 可读：

- `configs-before.private.json`、`configs-after.private.json`：前后完整快照。
- `rollback.private.sql`：仅当配置仍等于本次发布结果时恢复原配置和更新时间；并发编辑后会拒绝回退。
- `rollback-rehearsal.json`：临时表发布与回退结果。
- `publish.py`、`publish.private.sql`、`publication.json`：实际脚本和发布结果。

本次没有创建、删除或重启生产容器和镜像，没有清理历史包或数据。完成时根盘可用约 5.4 GiB，`/www` 可用约 3.1 GiB；这些是当时实测值。

本机验收日志与产物保留在 `D:/gh-pack/`：`screening-validation-20261026.log`、`screening-worker-20261026.log`、`screening-package-6.2.11.3.log`、`screening-public-download-62113.exe`，以及 `screening-bf01c192/goodhr5/local-agent-go-new/release/`。
