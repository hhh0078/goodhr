<!-- 文件作用说明：记录6.3.4.1双系统发布、同源码核对、安装包校验、更新入口与条件回退。 -->

# 本地程序 6.3.4.1 发布

2026-11-12。发布 BOSS 详情关闭检查超过一秒误停、备用关闭结果误判、聊天框及联系人侧栏重复关闭的修复。业务源码提交统一为 `7f75094c0c40c5012615e92c73299b53ad36d6bb`，功能验证见 `boss-close-verification-fix-20261112.md`。

## Windows

- 发布前发现 Windows 6.3.4.1 已由另一发布过程上线。核对 `/opt/goodhr-release/windows-6.3.4.1-7f75094c-20261112/publication.json` 确认源码提交、版本、大小和 SHA256 都对应本次修复。本次保留该更新记录，不覆盖同版本文件。
- 本机从公开链接完整下载 Windows ZIP，31,564,019 字节，SHA256 为 `d546daef0d4bec3ed620e68c968495d6c887249c2e8b72f73ebe2cc7ed7d9a89`。ZIP 正好包含 `GoodHR-Local-Agent-Setup-v6.3.4.1-windows-x64.exe` 和 `安装教程图片.jpg`；安装器32,023,273字节，SHA256为 `ec9ca3ffc672d55f4527e37a5854d3f6b8331fce9e3740b07db4b817863e73f3`，版本资源为6.3.4.1。
- 补充原生 Windows 构建入口 `.github/workflows/local-agent-windows-package.yml`，固定受测提交、正式域名、版本号，复用正式 PowerShell 打包脚本。原生 Windows BOSS/common 回归、安装器构建、ZIP内容校验、包内版本和正式域名检查、SHA凭据及 artifact 上传全部通过：[构建记录](https://github.com/hhh0078/goodhr/actions/runs/43159321335)。此独立重建产物仅作验收归档，没有覆盖已上线的 Windows ZIP。
- 首轮附加校验因 Inno Setup 的版本字符串含定长空格而失败，已将比较调整为两端去空白；第二轮完整成功，未降低版本或包内配置校验要求。本轮未在用户 Windows 电脑执行安装或整段招聘任务。

## macOS

- 从同一业务提交的 Git 归档隔离运行 `scripts/package-release.sh 6.3.4.1 build`，没有加入工作区其他修改。Go/npm 均检查并使用已有国内镜像。
- 应用、PKG 解包和 ZIP 解包共2452个文件、目录及链接的内容和权限一致。Go主程序及AppKit启动器均含arm64/x86_64；Apple和Intel的impit原生模块分别核对架构。Info.plist、build-info.json与内置程序帮助均核对版本，生产地址为 `https://goodhr.58it.cn`。
- PKG：60,753,977 字节，SHA256 `ee993c61e2eb83ee7109f266b3edfd02a2b262cac427191f9234dd70477daef1`。
- ZIP：62,594,494 字节，SHA256 `c0d6a4605ce58876f95a565dc02615f06cac4cc8ad7fb4d997835e0dbf22d38a`。
- PKG及ZIP都从公网HTTPS完整下载，HTTP200，字节数和SHA256一致。没有安装本次PKG；安装器未签名或公证。Intel真机安装、用户电脑覆盖升级及完整业务运行未验证。

## 下载及更新

- [Windows ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.1-windows-x64-d546daef.zip)
- [Mac 通用安装器](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.4.1-mac-universal-ee993c61.pkg)
- [Mac 通用应用 ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.4.1-darwin-universal-c0d6a460.zip)

仅在 `system.onboarding_config.local_agent` 首位新增Mac项，原三条记录及全部其他字段原样保留。公开更新接口和下载页均HTTP200并包含正确版本、两系统链接；使用真实前端版本选择函数验证Windows选到既有ZIP、Mac选到新PKG。6.3.4到6.3.4.1沿用可选补丁规则，不强制结束用户任务。

## 回退与收尾

本次私密材料位于 `/opt/goodhr-release/local-agent-6.3.4.1-20261112/`，目录0700、文件0600。包含完整配置快照、整行锁定条件更新、整行条件回退和验证凭据；在数据库临时表执行发布与回退SQL并确认整行还原后才切换正式配置，拒绝覆盖并发编辑。

没有重建或切换云端镜像、重启服务或删除历史文件。发布前后14个运行容器的ID、镜像与启动时间一致。结束时根分区可用6,455,545,856字节，`/www`可用12,605,435,904字节；无新增镜像、容器或网络。上传文件已移至摘要命名的最终路径，临时构建目录单独收尾，保留源码归档、正式包、配置快照、回退材料与验证记录。
