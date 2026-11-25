# macOS 安装器固定安装位置

## 故障与修复

2026-11-26 首次安装实测发现，官方 6.3.4.11 安装包在 `/Applications` 已没有旧版时，仍会找到源码 `release/GoodHR Local Agent.app`，把文件安装到该目录。安装后脚本固定打开 `/Applications/GoodHR Local Agent.app`，因此报错，且“应用程序”里没有 GoodHR。

原包 `PackageInfo` 顶层虽然有 `relocatable="false"`，但其 `<relocate>` 下仍声明了 GoodHR 的 bundle。这会允许安装器追随其他位置的同标识应用，不能只检查顶层属性。

- `packaging/macos/components.plist` 用 `BundleIsRelocatable=false` 固定应用位置，保留版本检查、标识检查和完整升级替换。
- `scripts/package-release.sh` 将该声明传入 `pkgbuild --component-plist`，构建后强制验证实际 PKG 元数据。
- `scripts/verify-macos-package.sh` 展开真实包，检查 `/Applications` 目标和空的 `relocate/bundle`。验证失败就停止发布，不移动源码、废纸篓或其他位置的应用作为补救。
- `packaging/macos/postinstall` 保留原启动流程；目标文件缺失时明确说明安装没有完成。

## 回归方法与平台差异

在 Mac 上运行：

```sh
bash scripts/test-macos-package.sh
bash scripts/verify-macos-package.sh /完整路径/待发布安装包.pkg
```

回归用真实 `pkgbuild` 构建两个合成包：固定位置的包必须通过，显式打开应用重定位的包必须被拦住。测试不执行安装、不请求管理员权限、不启动招聘任务。生产包仍需在有其他同标识应用副本的 Mac 上实际安装，确认 `/Applications` 目标和首次运行。

此修复只影响 macOS PKG 包装，Apple Silicon 和 Intel 共用同一份组件声明、安装前后脚本。未修改 Go、Worker 或 Windows ZIP 业务文件；Windows 继续走 `scripts/package-windows.ps1`。跨平台编译用于检查源码构建，不代替 Windows、Intel Mac 的实际安装验收。

## 本次安装修复候选

为保持业务版本真实，本次从已核验的官方 6.3.4.11 包提取完整 Payload，仅重新包装安装器。业务应用、Worker、依赖、资源及版本号保持 6.3.4.11，文件名添加 `installfix` 区分，不把旧二进制标成新业务版本。

- 官方输入包 SHA-256：`b05e8806f91dd18e9772c7e09e2335c8caa20f32e97c780a224dc99264583cb1`。
- 修复候选：`GoodHR-Local-Agent-Setup-v6.3.4.11-mac-universal-installfix.pkg`。
- 修复候选 SHA-256：`4ec7eaf341b90bf81536d137bfa130cb76e425918c9615bb1e2cd56c1817dd1f`。
- 原始和修复包分别展开后，逐文件比较应用内容、符号链接目标和权限；Agent 与原生 launcher 另验证 `arm64`、`x86_64` 双架构。
- 已完成：合成好坏 PKG 回归、真实候选包位置检查、全部 2262 项文件及符号链接内容和权限比对、双架构检查、Windows amd64 / macOS arm64 / macOS amd64 源码编译、脚本语法及差异检查，均通过。编译检查产物没有装入本次重包装候选。
- 未使用被失败安装覆盖的源码 `release` 目录作输入。实际安装结果和线上发布结果以本轮 WorkBuddy 验收记录为准。
