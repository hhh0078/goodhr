<!-- 文件作用：记录 Firefox 未签名包下线、Mozilla 审核签名、正式开放及本地程序自动准备插件的完整发布证据。 -->

# Firefox 签名门禁与正式开放记录（2026-10-12）

## 处理结论

标准版 Firefox 会拒绝长期安装未经过 Mozilla 签名的扩展。本轮先关闭旧的未签名下载入口，审核通过后只发布 Mozilla 返回的签名 XPI，并由公开配置的签名门禁和完整 SHA256 共同约束下载。

Firefox 与 Chrome 继续使用统一插件版本 `1.0.7`。Chrome 的普通用户安装教程已统一为“下载后直接拖入扩展管理页，无需解压”，不再显示旧的手工解压流程。

## 代码基线

- `042fb663edfa58c3fb6342f1b5b82f8e10d16242`：公开接口与下载页增加 Firefox 签名门禁。
- `deba7c8df5e5260d2fadbf501aa49a94fbffb869`：本地程序启动时自动准备签名 Firefox 插件，并统一 Chrome 无需解压说明。

Firefox 下载信息只有同时满足以下条件才会公开：

- `firefox_signed=true`；
- 下载地址为完整 HTTPS 地址并以 `.xpi` 结尾；
- SHA256 为 64 位小写十六进制；
- XPI 版本、产品名和固定扩展 ID 与 GoodHR 配置一致。

本地程序还会核对后台发布门禁、文件摘要、Manifest V3 后台入口和 Mozilla 签名信封。密码学签名真实性在正式发布环节单独验证，运行时依靠已验证文件的固定 SHA256 防止换包。

## Mozilla 审核与签名文件

- AMO 扩展标识：`cb2d26140f0b499fb7c3`。
- AMO 版本记录：`6567509`。
- 审核结果：通过；用户已在标准 Firefox 中完成安装测试。
- Mozilla 返回文件：`cb2d26140f0b499fb7c3-1.0.7.xpi`。
- 文件大小：`90,711` 字节。
- SHA256：`f8dffd54d70d87ffcfdf02c85498cfe9e90ee62fadd56326eff1012960fe1a61`。
- 扩展 ID：`resume-assistant@goodhr.58it.cn`。
- 签名校验：`META-INF` 文件摘要覆盖、PKCS#7/CMS 签名运算和 Mozilla AMO Production 证书链均通过；签名证书有效期覆盖本次发布。
- 除 Mozilla 签名元数据和等价的 Manifest 转义外，业务文件与送审输入一致。

等待审核期间留存的未签名文件只用于审计，不能作为正式下载包，也不能写入生产下载配置。

## 正式 OSS 与生产配置

- 正式下载：`https://oss2.58it.cn/GoodHR-Resume-Firefox-v1.0.7-f8dffd54.xpi`。
- 公网响应：HTTP `200`、`Content-Type: application/x-xpinstall`、`Content-Length: 90711`。
- 公网重新下载后的 SHA256 与 Mozilla 文件完全一致。
- `system.browser_extension.url_firefox` 已更新为正式 XPI 地址。
- `system.browser_extension.sha256_firefox` 已更新为完整摘要。
- `system.browser_extension.firefox_signed=true`。
- Chrome 1.0.7 地址与摘要保持不变。
- 配置发布证据和回滚材料：`/opt/goodhr-release/firefox-signed-1.0.7-f8dffd54`。

## 本地程序自动准备

Agent 正常启动时会读取公开插件配置，把签名 Firefox XPI 下载到临时文件，完成限量下载、SHA256、版本、扩展 ID、后台脚本、签名信封和安全路径校验，再原子切换到 `ExtensionsDir/goodhr-resume-assistant`。GoodHR 管理的 Camoufox 会在下次启动时自动加载该目录。

- 同版本、同摘要且目录内容未变化时幂等跳过。
- 下载或校验失败时保留上一版插件，不阻断 Agent 启动。
- Windows 与 macOS 共用该逻辑；本轮正式对外发布的可选安装包为 Windows `6.2.10.5`。
- 本地程序不会改普通系统 Firefox 的用户配置或企业策略。

## 测试结果

- Browser Extension：`90/90` 通过。
- Local Agent：`go test ./...`、`go vet ./...` 通过。
- 前端单元测试：`34/34` 通过。
- 前端 TypeScript 与生产构建：通过。
- 下载页 Playwright：`3/3` 通过；简历库 Playwright：`40/40` 通过。
- 下载页断言“无需解压”“直接拖到扩展管理页”，并断言不存在“加载已解压的扩展程序”。
- 正式 XPI 已完成真实线上配置下载与本地程序准备冒烟。

## 前端生产切换

- 正式前端镜像：`sha256:b46fd710015a11eb2893ed69b3b2279e42b212378ea0244ba3ab0cc84f423b8e`。
- 正式前端容器：`b5e1a403fe31dd0ea9b197ae182611d8d01e359d6f8827450537d8a757a0d0b4`，发布标签 `extension-install-deba7c8d`，启动后重启次数为 `0`。
- 原前端保留为 `goodhr-production-frontend-1-backup-extension-install-deba7c8d`。
- 服务器证据目录：`/opt/goodhr-release/extension-install-deba7c8d`，包含构建日志、候选验证、配置备份、应用与回滚 SQL、容器证据和一键前端回滚脚本。
- 切换前发现另一个未完成的 Mac 前端候选留下了未启动容器，并短暂造成 5175 空档；已先恢复旧前端，再把未启动容器保留为 `goodhr-production-frontend-mac-pending-20261012`，最后完成本次候选验证和正式切换。
- 后端、PostgreSQL 与 Redis 容器 ID、启动时间保持不变。

## 最终公网验证

- `/download` 返回 `200`，显示 Chrome“无需解压”、Firefox“已签名安装包”和 Windows `6.2.10.5`。
- Chrome 教程弹框显示从下载记录直接拖入扩展管理页，不包含旧的手工解压步骤。
- `/api/system/browser-extension` 返回签名 Firefox 地址、完整摘要和 `firefox_signed=true`，同时包含 `Cache-Control: no-store`。
- `/api/system/local-agent-updates` 首个 Windows 可用版本为 `6.2.10.5`。
- `system.app_config.local_agent_version` 仍为 `6.2.10.1`，因此 `6.2.10.5` 不强制更新。
- 未登录访问私有候选人接口仍返回 `401`。
