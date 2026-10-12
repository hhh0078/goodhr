<!-- 文件作用说明：记录 Windows 本地程序 6.2.10.6 恢复直接打开 GoodHR 页面、仅自动准备 Firefox 插件的构建、发布与回滚证据。 -->

# Windows 6.2.10.6 Firefox 专属插件准备修正

源码提交：`2aeb3b1e986b1904940e3c4ce860377d1bca8e59`。

## 修正范围

- 删除 Windows 安装器中的 Chrome 插件下载、准备、目录弹出和 `chrome://extensions` 引导。
- 删除 Agent 的 Chrome 专用准备命令参数。安装完成后只启动 GoodHR 小助手，由原有启动页直接进入控制台。
- 正常启动仍会下载、校验并安全解包 Mozilla 已签名的 Firefox XPI，交给 GoodHR 管理的 Camoufox 加载。
- 官网 Chrome 插件下载和手动安装教程继续保留，与本地程序安装流程互不影响。

## Windows 产物

- 正式下载：`https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.6-windows-x64-57710b0c.zip`。
- ZIP 大小：`30,694,237` 字节。
- ZIP SHA256：`57710b0c163fe2822e6ae4a66111c88af8c5b7d507c483d26ebf1c34561b21a0`。
- 安装器大小：`31,142,860` 字节，SHA256：`70d53758a70fdcfcd176eb1156e78e236dc64654b10528ff8dfe2be078ecffcf`。
- Agent SHA256：`bac3ad262595b8a0c10781bb8afad8ffbacb2d0367b59cf62c991e0c3dc6ac1c`。
- `go version -m`：`vcs.revision=2aeb3b1e986b1904940e3c4ce860377d1bca8e59`、`vcs.modified=false`。
- 安装器 `ProductVersion=6.2.10.6`；沿用现有未做 Authenticode 签名的发布方式。
- 外层 ZIP 只包含安装器、安装教程图片和安装说明，CRC 检查通过。

## 验证

- `go test ./...`、`go vet ./...`：通过。
- Browser Worker：`199` 项通过、`1` 项按既有条件跳过、`0` 项失败。
- 前端 TypeScript 检查与正式 Next.js 构建：通过。
- Inno Setup 正式安装器编译：通过。
- 静态回归断言安装器和 Agent 不再包含 Chrome 准备参数、`chrome://extensions` 或插件目录弹出入口；Firefox 启动同步仍位于业务服务启动之前，失败时保留旧版并继续启动。
- OSS 公网重新下载：HTTP `200`，大小和 SHA256 与本地产物完全一致。

## 可选更新与撤回

- `system.onboarding_config.local_agent` 当前共 `41` 条，首条为 `6.2.10.6`。
- `6.2.10.4`、`6.2.10.5` 已从公开更新列表移除，OSS 原文件仅保留作历史审计。
- `system.app_config.local_agent_version` 保持 `6.2.10.1`，本版不会触发强制更新。
- 服务器证据目录：`/opt/goodhr-release/firefox-only-agent-2aeb3b1e`，权限 `0700`；配置前后备份、摘要、应用 SQL、CAS 回滚 SQL 均为 `0600`。

## 前端与回滚

- 正式前端镜像：`sha256:76bfde98a01066af04144af7f267654b0dfd830f1ee994527bc0bc33af8e6447`。
- 正式前端容器：`021f3b2d44a1c6e407e51fe0eb17b0bd0bb3773a7e56a1b95f017234bab5cee0`，重启次数 `0`。
- 上一版容器保留为 `goodhr-production-frontend-1-backup-firefox-only-agent-2aeb3b1e`。
- 前端回滚脚本：`/opt/goodhr-release/firefox-only-agent-2aeb3b1e/rollback-frontend.sh`。
- 公网 `/`、`/download`、插件配置、本地程序更新配置均返回 `200`；未登录候选人接口返回 `401`。
