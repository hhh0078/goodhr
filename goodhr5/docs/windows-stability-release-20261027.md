<!-- 文件作用说明：记录 Windows 6.2.12.0 强制更新的源码、安装文件、验收、正式配置及回退证据。 -->
# Windows 6.2.12.0 稳定性版本发布

2026-10-27，按用户要求拉取最新代码，只重新打包上传 Windows x64，并与 Mac 使用相同版本号 `6.2.12.0`，作为强制更新版本发布。更新提示精确为：

> 极大的增强了运行稳定性。

本次正式程序来自干净主分支提交 `5499fc7d48ca8c2e118d05331706e8ddc3a0191b`，没有修改 Go、Worker 或云端业务源码。已有 Windows 浏览器修补包的验收与发布见 `local-agent-go-new/docs/camoufox-windows-window-r1-20261026.md`；Mac 自动换包代码与发布见 `local-agent-go-new/docs/mac-6.2.12-auto-camoufox-plan.md`。

## 正式文件

- [Windows 安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.0-windows-x64-831b57fe.exe)，31,224,165 字节。
- 安装器 SHA256：`831b57fe1256781a80686ae7a791cab17bcc373e8f26f52a46834b434a06c3ae`。
- [Windows ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.0-windows-x64-06097e91.zip)，40,197,818 字节。
- ZIP SHA256：`06097e918d282f4e41f591f2adfde01458b43e6107d5fff5fb4f439a16a457de`。
- 包内主程序 SHA256：`21587033e36eb18a20314f8220438777c3f49597257032d3ce4a5ee5c4dae3c0`。

通过既有 `scripts/package-windows.ps1 -Version 6.2.12.0 build` 构建，npm 使用 `registry.npmmirror.com`，Go 使用 `goproxy.cn,direct`，依赖版本未升级。Go 1.27.0 元数据确认 Windows amd64、CGO 关闭、上述源码提交和 `vcs.modified=false`；安装器 ProductVersion 与包内 build-info 均为 `6.2.12.0`，生产地址为 `https://goodhr.58it.cn`。

本次使用独立工作树，并通过短目录入口规避 Inno Setup 的长路径编译失败。最初日志目录使首轮构建标记为源码有改动，已移走日志并重新生成正式文件；首轮文件没有发布。最终清单为本机证据根目录与 evidence 中的 `artifacts.json`，首轮记录另存 `artifacts-before-clean-build.json`。

## 验收

- Go 全量测试和 `go vet ./...` 通过：34 个测试包、8 个无测试包，顶层 513 项通过、25 项因环境跳过、零失败；含子用例 884 项通过、35 项跳过。
- Worker 测试 217 项，215 项通过、2 项跳过、零失败。
- 最终精确包在独立数据目录和端口启动，健康接口返回 `6.2.12.0`，包内 Worker 启动、状态与停止均通过；核对包内入口与父子 PID，所有测试进程和端口均已退出。
- ZIP 2,264 个成员 CRC 校验通过；本机、服务器及独立完整公网 HTTP 200 下载的安装器/ZIP 大小和 SHA256 全部一致。
- 使用公开更新接口实际返回值执行原前端版本选择、强制更新和弹窗表达式：旧 Windows `6.2.11.3` 必须更新且不可忽略，新 Windows `6.2.12.0` 不重复提示；旧版岗位启动被拦截，新版放行。
- 官网首页、下载页与更新接口 HTTP 200。下载页同时显示新 Windows 安装器和原 Mac PKG；服务器本地正式后端 `/health` 返回 `ok=true`。

本次没有执行系统安装器覆盖安装，也没有操作真实招聘账号或招聘页面。当前 Windows 无 gcc、CGO 关闭，没有执行竞态测试；本次只发布 Windows，没有重新打包或声称执行 Mac 真机验证。

## 强制更新与配置保留

北京时间 **2026-10-27 23:44:05** 在服务器 `ldclouda31539` 完成事务发布。仅向 `system.onboarding_config.local_agent` 最前插入 Windows 独立记录，条数从 53 变为 54；记录只提供 Windows URL，SHA256 对应 EXE 本身。

现有前端按系统 URL 选择首条记录，比较版本前三段决定是否强制更新，因此 `6.2.11.3 → 6.2.12.0` 为强制更新。同版本的原 Mac `6.2.12.0` PKG、SHA256 与提示保持；原 53 条记录内容与顺序、其他 onboarding 字段、三平台 Camoufox r1 组件和 `system.app_config` 整行保持。

配置 JSONB 的 MD5 从 `ed887a72c70210bd06b9780682f7175b` 变为 `9899f1b6dde20048adf0b3db8189bbdf`。发布前后所有容器 ID、镜像引用保持，没有重启服务或清理旧包。

Windows 安装包仍只包含 Go 主程序、Worker 与依赖，不包含 Camoufox 浏览器。现有 Windows 启动流程不会自动执行 Mac 专属的按摘要换包：已有旧浏览器需要在后台点击“更新运行组件”，完成后彻底退出浏览器与 GoodHR 本地程序，再重新打开。Windows r1 组件配置保持已发布状态，SHA256 为 `c264719312c3344782a4e723511922d8ca872ebb27d8ea73e335ff35b1598bfa`。

## 备份与回退

服务器私有材料目录：`/opt/goodhr-release/windows-stability-6.2.12.0-5499fc7d-20261027/`。目录权限 700，文件权限 600，包含：

- `configs-before.private.json`、`configs-after.private.json`：两行配置的完整前后快照。
- `publish.py`、`publish.private.sql`、`publication.json`：已执行脚本、发布事务和结果。
- `rollback.private.sql`、`rollback-rehearsal.json`：整行并发保护的回退及实际演练结果。

正式执行前先在临时表运行同一发布和回退 SQL，逐行确认恢复原值。真实发布按原配置整行加锁比较；发布采用固定时间戳，回退只有当前整行与本次结果完全相同时才恢复原配置及更新时间，避免覆盖后续配置调整。

本机产物和完整证据保存在 `D:/gh-pack/windows-stability-62120-20261027/`，最终包的实际运行证据为 `evidence/final-smoke/package-smoke.json`，公开版本和弹框检查为 `evidence/public-update-verification.json`，完整下载检查为 `evidence/public-assets-verification.json`。最终服务器 `/www` 可用约 10.18 GB，本次没有创建临时容器或网络。
