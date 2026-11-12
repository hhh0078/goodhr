<!-- 文件作用说明：记录Mac6.3.2退出与重复打开修复、Win/Mac共用BOSS新旧入口兼容，以及安装包、公开更新、回退和验证范围。 -->

# Mac 6.3.2 与 BOSS 新旧页面兼容发布记录

## 本次结果

2026-11-11 从干净提交 `ec0192fc1306be724c9036df780bc8c03cb763d7` 构建正式 Mac 6.3.2，并通过宝塔 MCP 上传到 oss2 已有站点。原生启动器响应 Finder 重复打开事件，菜单栏 HR 提供正常退出；安装器识别并等待同身份旧程序退出，包含从项目 release 目录启动且占用默认端口的旧版。没有安装、覆盖或自动启动用户的正式客户端，用户自行点击网页更新后由安装器继续。

BOSS 的 `candidate.open_target` 保留完整选择器对象数组：先 `.row:has(> .base-info.join-text-wrap)`，再旧版 `.base-info.join-text-wrap`。共用 Go 流程逐条只发起一次点击请求，成功后停止；不确定结果先检查详情，已有弹窗不点击背后卡片。相同完整定位去重，旧单对象和独立备用入口保持兼容。

此流程和内嵌配置同时用于 Windows x64、Mac arm64、Mac amd64。Windows 用户需拉取包含该提交的最新 dev 重新打包；本次没有制作或上传 Windows 安装器，也没有改 Windows 更新记录。Mac 原生入口单独由 `packaging/macos/launcher.m` 实现，不进入 Windows 构建。

## 产物与校验

| 文件 | 字节数 | SHA256 |
| --- | --- | --- |
| [Mac PKG](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.2-mac-universal-b51a6ffe.pkg) | 60678303 | `b51a6ffeaa70c14e06352e53e488bb48a902e10b311824ba34cd43e71eb5f95f` |
| [Mac ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.2-darwin-universal-fb17b86a.zip) | 62518307 | `fb17b86a91e42c3c60b6d9528c94a45223ecbbe3c3b3d8ca8952340218b56696` |

使用既有 `scripts/package-release.sh 6.3.2 build`，npm 国内镜像与 Go 国内代理，构建目录是提交的 git archive，未带入其他未提交改动。PKG、ZIP 与构建出的应用逐文件一致，共 2250 项。版本和域名为 `6.3.2`、`production`、`https://goodhr.58it.cn`。Go 与 AppKit 启动器均包含 arm64、x86_64，Worker 同时包含 impit 的两个 Mac 原生架构。

- 三个 Go 目标编译通过；最后另从打包所用干净源码验证 Windows amd64 编译。
- 平台、协议、更新器及 Mac 主程序测试通过。单元测试验证入口逐条一次、成功停止、旧对象、重复入口、取消和断线。
- Mac arm64 隔离真实 Camoufox 测试同时覆盖新旧两种卡片，详情对应当前候选人，热搜、相邻姓名和招呼按钮点击为零。
- 原生启动器的真实 AppKit 测试验证重开及时回执、不重复创建业务进程，并等待两秒模拟收尾；安装器测试验证准确身份、错误身份不退出、安装路径和 release 监听进程。
- 打包后原生入口在随机端口、临时数据目录与本机模拟云端实际启动 Go 6.3.2，发送正常退出请求后入口与 Go 均退出、端口释放；未使用用户登录资料。arm64 impit 实际加载通过。
- 宝塔上传与站点文件再次核对大小和完整摘要；两个公开 HTTPS 地址均从本机完整下载，HTTP 200、字节数和 SHA256 一致。

安装包未签名。未覆盖安装现有客户端；Intel Mac 安装/运行与 Windows 真机运行仍未验证，交叉编译不代替这些验收。

## 网页更新与回退

正式数据库 `goodhr5.system_configs` 的 `system.onboarding_config.local_agent` 首位新增 Mac 6.3.2，`url_mac` 为 PKG，`url_win` 留空，SHA256 对应 PKG。原 63 条记录与其他配置完全不变，总数 64。更新仍需手动点击；按现有网页规则，前三段版本升高会显示必须更新提示，不会自行下载安装。

[公开更新接口](https://goodhr.58it.cn/api/system/local-agent-updates) 已核对版本、Mac 地址及 SHA256；移除新增项后，其余公开记录规范化摘要为 `c23de527587e586d658e849e27a01be35404fd47bc67e235fb08b859cf32cfe0`，与发布前一致。[官网下载页](https://goodhr.58it.cn/download) HTTP 200，包含 6.3.2 及正确安装包链接。

服务器私密目录 `/opt/goodhr-release/mac-6.3.2-b51a6ffe-20261111/` 为 0700，材料为 0600：完整配置前后快照、摘要加锁的 `update.sql`、只允许恢复本次状态的 `rollback.sql`、文件清单、构建及公网完整下载校验凭据。配置更新前摘要 `e35a3f8d56b3714484211ffb726221ac632e4afba78ada9b9d95403f2efdf14c`，更新后 `67db1f4033c7fa998397687ebf9dce4444fa47933e47c2e08af0f766fe87ae74`。历史包、配置和回退材料保留，不作为缓存清理。

## 空间与收尾

完成时根盘可用 6311444480 字节，约 5.88 GiB；oss2/Docker 所在 `/www` 可用 4367278080 字节，约 4.07 GiB，使用率 90%。发布前分别为 6311821312 与 4540665856 字节；并发服务写入也可能影响差值。发布前后 14 个运行容器的名称和镜像全部一致。本次没有构建云端镜像、迁移数据库、切换组合或重启服务。

上传的临时源文件已移入站点发布位置。仅清理本次干净源码构建和解包验收临时目录；本地 release 保留两个安装包、构建日志、摘要及验收 JSON，保留其他任务和用户数据。
