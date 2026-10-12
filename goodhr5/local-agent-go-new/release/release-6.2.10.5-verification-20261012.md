<!-- 文件作用说明：记录 Windows 本地程序 6.2.10.5 自动准备签名 Firefox 插件的构建、可选发布、验证与回滚证据。 -->

# Windows 6.2.10.5 签名 Firefox 插件自动准备

> 发布状态：已于 2026-10-12 撤下。Firefox 自动准备逻辑本身保留，但安装器仍继承了 Chrome 插件准备和扩展管理页引导；公开更新列表已移除，修正版为 `6.2.10.6`。

源码提交：`deba7c8df5e5260d2fadbf501aa49a94fbffb869`，已推送功能分支并快进合并到 `main`。

## 功能范围

- Agent 正常启动时读取 `system.browser_extension` 公开配置，只接受后台标记已签名、HTTPS XPI 地址和完整 SHA256 的 Firefox 发布。
- 下载后核对版本、产品名、固定扩展 ID、后台入口、Mozilla 签名信封和目录摘要，再安全解压并原子切换到 GoodHR 管理的扩展目录。
- 同版本同摘要且文件未变化时跳过；新版下载或校验失败时保留旧目录，Agent 继续启动。
- GoodHR 管理的 Camoufox 下次启动会自动加载扩展。普通系统 Firefox 不写用户配置或企业策略。
- Chrome 官网安装包仍由用户主动下载安装；本地程序不应下载、准备或打开 Chrome 插件。该边界已在 `6.2.10.6` 修正。

## Windows 产物

- 正式下载：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.5-windows-x64-8df505b7.zip`。
- ZIP 大小：`30,681,924` 字节。
- ZIP SHA256：`8df505b7a5c10b13dc84ac9841eed37804da2c63126a062a277270b9d2c58a22`。
- 安装器 SHA256：`eb6f829dd84ef43665b4e73031cf590d70a6f4c314b772b3f66b3da7f2039ba0`。
- Agent SHA256：`e11e2c9f25beacdd6589d409312a48d2008c190bb49d959ae6fe5511c08d5486`。
- `go version -m`：`vcs.revision=deba7c8df5e5260d2fadbf501aa49a94fbffb869`、`vcs.modified=false`。
- 安装器 `ProductVersion=6.2.10.5`。
- 外层 ZIP 只包含安装器、安装教程图片和安装说明三项，CRC 与内嵌哈希均通过。
- 安装器沿用现有未做 Authenticode 签名的发布方式。

## 测试

- `go test ./...`：通过。
- `go vet ./...`：通过。
- 真实 Mozilla 签名 XPI 的线上准备冒烟：通过。
- 同版本幂等、签名门禁、错误扩展 ID、缺失签名信封、新版失败保留旧版、安全解压和目录原子替换测试：通过。
- Worker 和 Inno Setup 正式构建：通过。
- 最终说明中包含 Chrome“无需解压”，不存在旧的“加载已解压”用户文案。

## 可选更新配置

- `system.onboarding_config.local_agent` 从 `41` 条增加到 `42` 条，前插 Windows `6.2.10.5`。
- 版本地址、完整 SHA256 和说明均已通过公开更新接口复核。
- `system.app_config.local_agent_version` 保持 `6.2.10.1`，本版不会触发强制更新。
- 更新事务锁定唯一配置行，断言全部历史和无关字段完整保留；回滚 SQL 只移除摘要、地址和版本完全匹配的本次记录。

## 服务器与恢复材料

- OSS 目标：`/www/wwwroot/oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.5-windows-x64-8df505b7.zip`，权限 `0644 root:root`。
- 公网响应：HTTP `200`、`application/zip`、`30,681,924` 字节；公网重新下载 SHA256 完全一致。
- 私密证据目录：`/opt/goodhr-release/extension-install-deba7c8d`，权限 `0700`。
- 配置备份、备份摘要、应用 SQL、回滚 SQL 和执行日志权限均已收紧。
- 前端回滚脚本会停止本次容器并恢复 `goodhr-production-frontend-1-backup-extension-install-deba7c8d`。

## 正式验收

- `https://goodhr.58it.cn/api/system/local-agent-updates` 返回 `6.2.10.5`、目标地址和完整摘要。
- `https://goodhr.58it.cn/download` 展示 Windows `6.2.10.5`，更新记录默认仍只显示最近三条。
- 下载页和 Chrome 安装教程弹框均显示“无需解压”。
- Firefox `1.0.7` 正式 XPI 仍返回 `application/x-xpinstall`，大小和摘要不变。
- 前端、后端、PostgreSQL、Redis 均运行正常，私有候选人接口匿名访问仍返回 `401`。
