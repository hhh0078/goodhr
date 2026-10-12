<!-- 文件作用说明：记录 macOS 通用本地程序 6.2.10.6 的修复、构建、上线验证与回退材料。 -->

# macOS 6.2.10.6 发布验证

源码已推送到 Gitee 和 GitHub 主分支：`ff5d23cfac77d52e4dd0821a94fe10c294fb1684`，Mac Agent 代码提交为 `bdb0b4e5`。构建在合并 Windows Firefox 专属更新后的 `91dbd3fa` 代码上完成；之后合入的变更只涉及云端页面、默认配置和发布 SQL。

## 本次修复

- 下载页在 Mac 卡片和安装说明中提示未签名安装包可能出现“移到废纸篓”，并说明到“系统设置 → 隐私与安全性”底部点击“仍要打开”。
- Mac 应用声明 `LSBackgroundOnly=true`；Agent 继续打开网页控制台，无窗口的应用不再占用 Dock 等待窗口。升级前脚本只停止当前登录用户、从正式安装路径启动的旧 Agent，等待它和本地端口退出后再覆盖。
- Agent 启动时准备已签名 Firefox XPI；Worker 在持久 Firefox Profile 启动前校验 SHA256，并将 XPI 原包装入固定扩展 ID。旧 Chrome `service_worker` 目录不会再阻断 Firefox 扩展加载。Chrome 插件不由本地程序自动准备或安装。

## 安装包与组件

- 正式 PKG：[GoodHR Mac 6.2.10.6](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.6-mac-universal-b7886055.pkg)，大小 `58,221,498` 字节，SHA256 `b7886055e93995fc626f0f67961e73976eb4288fdb700b6d28ff54a6c6239412`。公网完整下载后重新计算 SHA256，结果一致。
- 包内 Go 可执行文件同时包含 `arm64` 和 `x86_64`；Worker 包含两种架构的 `impit` 原生依赖。PKG 包含 `preinstall`、`postinstall`，版本为 `6.2.10.6`，签名状态为 `no signature`。
- Firefox 签名 XPI `1.0.7` 的 SHA256 为 `f8dffd54d70d87ffcfdf02c85498cfe9e90ee62fadd56326eff1012960fe1a61`。Apple 芯片与 Intel 的 Node、Camoufox，以及通用 Mac OCR，均沿用已发布的 OSS2 组件地址；六个组件/插件 URL 的公网 HEAD 均返回 `200`。

## 验证

- Mac Agent `go test ./...`、`go vet ./...` 通过；Browser Worker `205/205` 通过；前端 `34/34` 测试及正式构建通过；云端后端 `internal/httpapi` 全部测试和 `go vet` 通过。
- `plutil -lint`、安装脚本 `sh -n`、PKG 脚本与源码逐字节比较、`git diff --check` 通过；隔离假进程测试确认安装脚本只停止目标 Agent。
- 最终 App 的 Apple 芯片切片和 Intel 切片（Rosetta）分别在隔离数据目录及端口启动，两个 `/health` 均返回版本 `6.2.10.6`；两者下载的 Firefox XPI 均与正式 SHA256 一致。测试进程已退出，未覆盖当前用户安装的 App。此前在独立 Firefox Profile 的真实 Camoufox 启动中，`extensions.json` 显示 GoodHR `1.0.7` 为 `active=true`、`appDisabled=false`、`userDisabled=false`。
- Intel 切片是在 Apple 芯片 Mac 的 Rosetta 下运行；未使用实体 Intel Mac 硬件测试。

## 生产配置与页面

- 发布 SQL：`goodhr5/cloud/backend/docs/mac-local-agent-6.2.10.6-production-config-20261012.sql`，事务内锁定唯一配置行，只在 `system.onboarding_config.local_agent` 首位新增 Mac 记录。其余 JSON（含 Windows 列表和 Mac/Windows 运行组件）按断言保留。
- 公开 `api/system/local-agent-updates` 从 `41` 条变为 `42` 条：Mac `6.2.10.6` 在首位，原 Windows `6.2.10.6` 在第二位。下载页 Mac 按钮已指向上述 PKG，且安全提示完整；Windows 下载地址未变。

## 线上恢复材料

- 正式前端容器为 `goodhr-production-frontend-1`（短 ID `a6c473c0d960`），镜像 `goodhr-production-frontend:mac-agent-ff5d23cf`，镜像 ID `sha256:cde67df5557cac24f3f3b1e3bf11d3247a54427c6f2a90aeaecc10fbe1e9079`。旧容器保留为 `goodhr-production-frontend-1-backup-mac-agent-ff5d23cf`；前端回退脚本在 `/www/wwwroot/goodhr-production/backups/frontend-mac-agent-ff5d23cf-20261012/rollback-frontend.sh`。
- 生产 `system_configs` 发布前备份在 `/www/wwwroot/goodhr-production/backups/mac-agent-config-6.2.10.6-20261012/system_configs-before-mac-62106.dump`；备份目录权限 `0700`、文件权限 `0600`，`pg_restore -l` 可读取。发布前目标 JSON 的 MD5 为 `95b1f38be5fbf80ccb6f637dafdea5c5`。事务输出 `BEGIN`、`DO`、`42` 条校验结果、`COMMIT`，并断言 `local_agent` 以外的 JSON 保持不变。
- 只撤回本次 Mac 记录的审阅 SQL 为 `goodhr5/cloud/backend/docs/mac-local-agent-6.2.10.6-production-rollback-20261012.sql`；该文件尚未执行。发布后宝塔面板 `/xterm` 与 `/files` 返回 HTTP `404`，因此暂未将该 SQL 复制进服务器私有目录，也未能停止隔离测试用的 `5178` 候选容器；正式前端、后端、下载页和更新接口复查仍正常。
