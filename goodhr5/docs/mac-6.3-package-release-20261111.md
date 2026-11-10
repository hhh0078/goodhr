<!-- 文件作用说明：记录用户已打包的 Mac 6.3 产物通过宝塔 MCP 上传、正式下载配置更新、校验结果和回退位置。 -->

# Mac 6.3 安装包发布记录

## 发布结果

- 2026-11-11 发布 Mac 6.3，为可选更新。
- 使用用户提供的 `local-agent-go-new/release/` 下已有 PKG、ZIP，未重新构建客户端。
- 通过全局已配置的宝塔 MCP 调用 `HtmlProjectInfo`、`Upload` 和 `Bash`；上传命令按 MCP 返回的固定服务器公钥执行。
- oss2 站点根目录确认为 `/www/wwwroot/oss2.58it.cn`，两个文件均直接放在根目录，权限为 `0644`。
- 正式数据库 `goodhr5` 的 `system_configs` 表、`system.onboarding_config` 配置中，新增 Mac 6.3 记录并放在首位。`url_mac` 指向 PKG，`sha256` 使用 PKG 摘要，`url_win` 留空。
- 更新记录由 61 条变为 62 条；全部原记录、Windows 最新 6.2.12.5 和其他配置保持原样。未构建云端镜像或变更容器。

## 下载文件

| 文件 | 字节数 | SHA256 |
| --- | --- | --- |
| [Mac PKG 安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3-mac-universal-139cd832.pkg) | 60433882 | `139cd8321fcf7470fbca7de6ec619988f76891c0baf387ae65cbf36cd45f5e21` |
| [Mac ZIP 应用包](https://oss2.58it.cn/goodhr-local-agent-v6.3-darwin-universal-f7db9585.zip) | 61512073 | `f7db95857d1bf78b6f95f72a5a6e251c02c49d1f10e3ceedd7db56efcd841514` |

文件名附加摘要前八位，以便保留旧产物并区分重新打包的文件。Mac 包不加入 Windows 安装教程图片。

## 验证结果与范围

- PKG 内版本为 `6.3`，`build-info.json` 的环境为 `production`，域名为 `https://goodhr.58it.cn`。
- PKG 中 Go 主程序通过 `lipo -verify_arch arm64 x86_64`，包含 Apple、Intel 对应的 Worker 原生依赖；ZIP 中版本信息、主程序摘要和两种依赖的元数据与 PKG 一致。
- ZIP 完整性检查通过。客户端二进制内嵌源码提交为 `fec1eff52c6734a1d0ecf3e3364e881eb244405a`，`vcs.modified=true`，本次使用该现有产物直接发布。
- 宝塔上传返回 `expected_sha256_matched=true`；发布到站点根目录前后再次校验文件大小和 SHA256。
- 从本机对两个 HTTPS 地址分别完整下载，均为 HTTP 200，字节数和 SHA256 与本地一致；未使用部分下载代替完整校验。
- [公开更新接口](https://goodhr.58it.cn/api/system/local-agent-updates) 返回 Mac 6.3 的正确 PKG 地址和 SHA256。移除新增记录后，其余 61 条公开记录的规范化 JSON SHA256 为 `5085b07a5cb68ce7fbd1888c19a4593fbce6ad7cc1fd71c7617c8377959251a1`，与更新前一致。
- PKG 未签名；本次未执行 Apple 芯片或 Intel 设备的真实安装、启动、浏览器任务或自动更新验证。

## 配置备份与回退

私密材料保存在服务器 `/opt/goodhr-release/mac-6.3-139cd832-20261111/`，权限限制为仅所有者访问，不进入 Git：

- `system-onboarding.before.json`、`system-onboarding.after.json`：完整配置行前后快照。
- `update.sql`：锁定并核对完整旧配置后，事务新增 Mac 6.3；配置有并发变化时停止。
- `rollback.sql`：只在配置仍等于本次发布结果时恢复旧配置，避免覆盖后续编辑。
- `release-files.json`、`public-download-receipts.json`：上传文件与公网完整下载校验凭据。

更新前配置快照 SHA256 为 `475e842a2369c3ac1ad8c6148fd7c20685a531e032da1958d1a8c74ffe795cde`。回退时使用上述条件 SQL 恢复配置，保留新旧安装包供核查。

## 空间和服务状态

- 完成时根分区可用 `7726858240` 字节（约 7.20 GiB）。
- oss2 与 Docker 所在 `/www` 分区可用 `2954567680` 字节（约 2.75 GiB，使用率 93%）。
- 发布前后 14 个运行容器的名称和镜像一致；本次没有构建镜像、创建隔离容器或重启服务。
- 本次上传的临时源文件已移入站点发布位置；配置快照、回退 SQL、校验记录和历史安装包保留。
