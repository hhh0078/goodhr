<!-- 文件作用说明：记录 macOS 通用本地程序 6.2.10.7 的中文显示修复、安装包验证和生产下载配置。 -->

# macOS 6.2.10.7 发布验证

安装包基于 `c4fc7b3e` 的本地程序代码加本次修复构建；之后主分支新增的 `70cdf672` 只涉及云端支付统计，不改变包内代码。本次源码与验证材料一起提交，具体版本以本文件的 Git 记录为准。

## 本次修复

- Mac Agent 在 Camoufox 启动前修正 `camoufox.cfg`，提前锁定独立 GPU 进程为关闭状态，以处理 [Mozilla 2032466](https://bugzilla.mozilla.org/show_bug.cgi?id=2032466) 涉及的字体渲染问题。配置写入使用同目录临时文件和原子替换，保留原有内容及文件权限；失败会在 Worker 启动前返回错误。
- Mac Worker 通过官方 `camoufox-js` 的 `fonts` 参数加入组件内置的 `Noto Sans SC`；SDK 会与目标系统默认字体合并。同时设置 `zh-CN` 的中文回退字体。Windows 的启动参数和浏览器资源保持原样；已有 Profile 的指纹与 Cookie 不会因本次修复重建。

## 安装包

- 正式未签名 PKG：[GoodHR Mac 6.2.10.7](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.7-mac-universal-4f933881.pkg)，大小 `58,233,320` 字节，SHA256 `4f9338811b85b6da2fc447bb160f845fb8b6878573fd61a2774c4dadf12c2f31`；公网完整下载与本地文件摘要一致。
- App 内 Go 程序是 `arm64`、`x86_64` 通用二进制。PKG 展开后的程序与 Worker 修复文件，均与实际测试 App 的对应文件逐字节相同。
- 两个架构的最终 App 在隔离数据目录启动后，`/health` 都返回 `6.2.10.7`，并实际修正各自隔离组件的早启动配置。Intel 切片在 Apple 芯片 Mac 的 Rosetta 下验证；尚未在实体 Intel Mac 上测试。

## 中文显示验证

- 使用 [smoke-macos-chinese.mjs](../scripts/smoke-macos-chinese.mjs) 和可见浏览器测试 Apple 芯片及 Rosetta Intel 组件。每个架构分别以 Windows、macOS 两种浏览器指纹打开隔离页面；每种指纹对 8 种字体、UTF-8、GB18030、Brotli、gzip 完成 `32` 次中文文本断言和 `4` 张截图。BOSS 直聘公开页中文显示正常，隔离浏览器 GPU 进程数均为 `0`。
- 原先启用 GPU 的隔离 Intel 旧 Profile 也通过 `32` 次文本断言和 `4` 张截图；修正后 GPU 进程数为 `0`。证据分别见 `/tmp/goodhr-font-final-arm64-render/evidence.json`、`/tmp/goodhr-font-final-x64-render/evidence.json`、`/tmp/goodhr-font-final-x64-old-profile/evidence.json`。
- Go `browser/runtime/config` 测试通过，`runtime` 的 `go vet` 通过；Worker `208` 项测试通过；Windows 目标编译通过。Windows 尚未进行真机测试。

## 生产下载配置与回退材料

- 宝塔文件页上传成功后，公网完整下载重新计算 SHA256，结果与本地包一致。
- 宝塔终端未能建立可用连接，因此通过已登录 GoodHR 超管的系统配置页面发布。保存前刷新配置并复制完整编辑器文本，只在 `system.onboarding_config.local_agent` 首位加入 Mac `6.2.10.7`。保存后重新读取全部字段，按 JSON 内容深比较，与预期完全一致；Windows 记录、运行组件和其余字段没有变化。
- 公开 `https://goodhr.58it.cn/api/system/local-agent-updates` 从 `42` 条变成 `43` 条；去掉首条后与发布前的全部列表逐项相同。Windows 最新版本仍为 `6.2.10.6`，URL 与 SHA256 未变。
- 官网 `https://goodhr.58it.cn/download` 已显示 Mac `6.2.10.7`，Mac 按钮指向上述 PKG；Windows 卡片仍为 `6.2.10.6`，未签名安装说明仍完整。截图位于 `/tmp/goodhr-mac-6.2.10.7-download-proof.png`。
- 配置发布前、后备份位于本机 `/tmp/goodhr-onboarding-before-mac-6.2.10.7.json`、`/tmp/goodhr-onboarding-after-mac-6.2.10.7.json`，权限 `0600`。截图为 `/tmp/goodhr-mac-6.2.10.7-upload-proof.png` 和 `/tmp/goodhr-mac-6.2.10.7-config-proof.png`。
- `cloud/backend/docs/mac-local-agent-6.2.10.7-production-config-20261013.sql` 与对应 `production-rollback` SQL 是经过代码审阅的数据库备用材料，本次没有执行；回退文件只删除内容匹配的这一条 Mac 记录，不覆盖后来新增的记录。
- 安装后请完全退出旧 GoodHR Firefox，再重新打开；已有打开的浏览器无法在运行中更换早启动配置。未签名包仍可能需要到“系统设置 → 隐私与安全性”点击“仍要打开”。
