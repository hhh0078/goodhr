<!-- 文件作用：记录 Mac 6.2.12.2 可选更新包的功能、源码对应关系、双架构隔离验证和正式发布边界。 -->

# Mac 6.2.12.2 发布验收

构建日期：2026-11-02。包内程序来自干净提交 `f1737c16`，包含 `f7895186` 的平台适配及本次双平台共用书签修复。使用现有脚本：

```sh
GOODHR_NPM_REGISTRY=https://registry.npmmirror.com \
GOPROXY=https://goproxy.cn,direct \
./scripts/package-release.sh 6.2.12.2 build
```

## 更新功能

- 新增前程无忧人才推荐、搜索人才页面的主动招呼适配。
- 新增智联搜索人才适配，保留原推荐页支持。
- 复用已经打开的支持页面，无需选择页面；URL 和选择器兼容旧格式及对象、数组。
- 兼容宽窄窗口按钮，操作前避让固定页头、底部客服和积分悬浮入口，并核验当前候选人身份。
- 配合本次云端更新，编辑岗位时可修改平台。
- 默认书签新增前程无忧；只移除书签栏中标题及地址均匹配旧默认 `goodhr5.58it.cn` 根入口的节点，保留用户改名、子路径、其它地址和文件夹。

前程独立自动回复、附件下载没有扩展。本次为 Mac 可选更新，Windows 安装包由用户后续处理。

## 安装产物

| 产物 | 大小（字节） | SHA-256 |
| --- | ---: | --- |
| `GoodHR-Local-Agent-Setup-v6.2.12.2-mac-universal.pkg` | 59,315,610 | `0e8ee2909af348d0adfbd53ee9d9a4a0b89b3f7a0eba9b502db96b0fe6c34b8d` |
| `goodhr-local-agent-v6.2.12.2-darwin-universal.zip` | 61,147,330 | `89d2120916f95bc787a4108dc4f4871362f2d53bdcdc8b4e45ac47cb4f51d986` |

产物保存在本目录上级的 `release/`，构建日志为 `/tmp/goodhr-mac-62122-build.log`。已使用带摘要前八位的唯一文件名发布，没有覆盖旧包：

- [Mac PKG 安装包](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.12.2-mac-universal-0e8ee290.pkg)
- [Mac ZIP 压缩包](https://oss2.58it.cn/goodhr-local-agent-v6.2.12.2-darwin-universal-89d21209.zip)

两个公开地址均完整下载并核对 HTTP 200、字节数及 SHA-256。线上 Mac 更新记录的 `url_mac` 和 `sha256` 均对应上表 PKG 本身，`url_win` 留空，没有发布 Windows 安装包。

## 验证结果

- PKG 展开后的 App、打包 App 与 ZIP 中的文件内容、符号链接及权限共 2,240 项完全相同；安装前后脚本与源码一致，未执行系统安装器。
- `Info.plist` 的两个版本字段及 `build-info.json` 均为 `6.2.12.2`，环境为正式环境，公开服务地址为 `https://goodhr.58it.cn`。
- Go 程序同时包含 `arm64`、`x86_64` 切片；按同样正式参数重编，两份程序与包内对应切片逐字节一致。两份切片均完整内嵌当前 `51job/config.json` 及最新书签脚本。
- Worker 包含两个架构的 `impit` 原生模块；使用各自真实架构的 Node 22.19.0 加载成功。Worker 自身包版本仍为 `0.1.0`、健康协议版本为 `v1`，应用发布版本为 `6.2.12.2`，三者用途不同。
- 最终 PKG 的主程序在 Apple Silicon 原生与 Intel/Rosetta 两个隔离目录运行，`/health` 均返回 `6.2.12.2`。各自独立端口的最终包 Worker 健康检查及 Go→Worker 健康桥通过，包内选择器数组解析也通过。
- 两架构均由最终主程序在隔离 Profile 中生成书签资源，确认前程入口存在、GoodHR 地址正确；对实际生成的扩展脚本运行两项旧书签删除、用户节点保留和幂等性回归，全部通过。扩展只有 `bookmarks` 权限，没有网页内容脚本。
- 未绑定的隔离程序仍会阻止浏览器启动，先要求完成原有 Camoufox 检查；此次没有绕过启动门闩、启动浏览器或招聘任务，没有使用真实招聘页面、账号或 Profile，没有请求正式本地端口 `43129`、`39881`。
- 本次创建的两个 Agent 和两个 Worker 均正常退出，退出码为 0。未替换 `/Applications` 内的正式程序。
- `pkgutil --check-signature` 为 `Status: no signature`，沿用现有未签名包方式，没有新增签名或公证。Intel 验证为 Rosetta，不代表实体 Intel Mac 或实际系统安装授权流程已验证。

包内容证据为 `/tmp/goodhr-mac-62122-audit/artifact-audit.json`；双架构运行及清理证据为同目录 `runtime-audit.json`、`runtime-arm64/`、`runtime-x64/`。构建前磁盘可用约 4.5 GiB，验收后约 3.1 GiB，未清理其它任务或用户文件。

## 非强制更新规则

现有前端只在前三段版本升高时强制更新；`6.2.12.0 → 6.2.12.2` 只增加第四段，是可忽略更新，岗位启动不被此更新拦截。已使用当前前端函数及发布前公开配置做断言：前置仅含 Mac 地址的 `6.2.12.2` 记录后，Windows 仍选择原 `6.2.12.1`，完整 Windows 记录不变。

没有新增或修改强制更新逻辑。更旧版本已有的 `6.2.12` 必要升级基线保持；线上配置已只前置新 Mac 记录，共 55 条，保留全部 54 条旧记录的内容、顺序及其它 onboarding 字段。Windows 最新记录仍为 `6.2.12.1`，完整记录与发布前一致。默认书签代码没有系统条件分支，已同步到两端共用源码；升级后关闭并重新打开 GoodHR 火狐浏览器时应用。

公开下载页、平台配置及更新接口回读均通过。证据在 `/tmp/goodhr-publish-20261102/public-mac-artifacts-verified.json`、`public-release-verification.json`；服务端保留 `/opt/goodhr-release/platforms-2176fac5-20261102/mac-update-publish-proof.json` 及更新前后私密配置备份。

## 升级时 Worker 的复用边界

本次只读核对了正常 PKG 升级链路，没有新增 Worker 哈希或版本握手，也没有修改旧进程的生命周期逻辑：

1. [安装前脚本](../packaging/macos/preinstall)向当前登录用户的正式旧 Agent 发送退出信号，并等待旧 Agent 进程退出及本地接口端口释放，随后安装器才覆盖应用。
2. 旧 `6.2.12.0` 的 [Application.Run](../internal/bootstrap/application.go)已经包含收尾顺序：停止任务、关闭本地接口、关闭浏览器、保存下载记录，再停止自有 Worker，最后退出 Go 主程序。[Worker Manager.Stop](../internal/browser/process/manager.go)先发送中断信号，等待后仍不退出则强制结束，并再次等待退出。因此正常完成该链路时，不会把仍存活的自有旧 Worker 留给新版本复用。
3. 新程序的 [defaultWorkerEntry](../internal/config/config.go)优先选择新 App 内的 `Contents/MacOS/worker/dist/main.js`；旧 Worker 已退出后，由该路径启动本次新包中的 Worker。

异常残留不能写成已自动解决：[Manager.Start](../internal/browser/process/manager.go)目前只检查健康状态 `ok`，没有比较监听者的包哈希或应用版本；没有进程句柄的孤儿 Worker 也不会被 `Manager.Stop` 停止。[SyncStartupCamoufox](../internal/runtime/startup_camoufox.go)在浏览器版本及 SHA 已匹配时会跳过换包前的旧 Worker 停止检查，所以旧程序崩溃、异常结束或组件停止失败后，仍可能遗留可响应的旧操作进程。

本次按正常 PKG 升级路径发布，没有扩大到异常孤儿进程的自动接管或结束。若异常退出后出现组件未退出、页面能力与新版本不一致等情况，应先完整退出旧 GoodHR 和浏览器，再重新打开程序；仍有残留时，可重启电脑后再运行。不要把只关闭后台网页当作已经退出本地程序。
