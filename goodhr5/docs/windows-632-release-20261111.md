<!-- 文件作用说明：记录 Windows 6.3.2 的源码、专用工具打包、发布校验和回滚位置。 -->

# Windows 6.3.2 发布验收

- 本次从最新 dev 固定提交 `4389cce4ec4ae139d27a2d68b75cf668b9833310` 打包，没有修改业务源码。Windows 从 6.3 更新到 6.3.2，对齐已发布的 Mac 6.3.2；沿用用户确认的现有升级规则。
- 本次 Windows 行为更新：BOSS 基础信息鼠标穿透修复、新旧候选人卡片详情入口兼容，以及详情已打开或页面失联时停止重复点击。上游 Mac 启动器与退出修复随源码保留，本轮只发布 Windows。
- 使用项目专用入口 `goodhr5/local-agent-go-new/package-tools/package-windows.bat 6.3.2 build`，正式环境 `https://goodhr.58it.cn`。成品构建信息为 6.3.2、Windows x64、`vcs.modified=false`。

## 成品

| 成品 | 字节 | SHA256 |
| --- | --- | --- |
| [教程 ZIP](https://oss2.58it.cn/goodhr-local-agent-v6.3.2-windows-x64-181bbde9.zip) | 31544713 | `181bbde9e2956c8eee259037a211edc6440ef11aa9f536d2cccee4ac01064846` |
| [安装器 EXE](https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.3.2-windows-x64-40ea5252.exe) | 32003228 | `40ea5252fc4cb1bb574cd17076870e238d3d8aa1bcef54b296bd8b8c4c783fc6` |

ZIP 内只有安装器与安装教程图片，CRC 及内部安装器与外部成品逐字节一致。最初误把更新记录配为 EXE，已按专用工具分发要求修正为 ZIP URL 与 ZIP SHA256；当前线上下载及更新都使用上表 ZIP。

## 验证

- `go test ./...` 通过，41 个有测试包；`go vet ./...` 通过。包含详情定位展开、新旧入口、结果不确定与会话失联的回归。
- Worker 366 项：364 通过、2 跳过、0 失败。
- Windows 最终成品使用独立数据目录、50772/50773 端口启动，健康接口返回 6.3.2 和 ok，随后结束本轮进程。没有覆盖安装、操作真实招聘账号或发送消息。
- Mac arm64/amd64 交叉编译通过；未执行 Mac 真机启动、安装或退出验证，不把上游 Mac 说明当作本轮实测。
- 本机产物、摘要及启动证据：`D:/gh-pack/windows-632-20261111/`；构建与测试日志：`D:/gh-pack/win-632-*.log`。
- 两份成品通过公网完整下载，HTTP 200、字节数和 SHA256 与本地一致；正式更新接口和下载页均显示 Windows 6.3.2。

## 服务器配置和回退

- 服务器：`ldclouda31539`。只新增本轮 Windows 条目，保留 Mac 6.3.2、历史记录及其顺序、其他 onboarding 字段和完整 `system.app_config` 行。
- 配置快照、发布 SQL、精确回滚 SQL 及临时表发布/回滚演练：`/opt/goodhr-release/windows-6.3.2-4389cce4-20261111/`。
- 本轮不部署或重启云端服务，不清理旧文件、容器、镜像或数据卷。发布前根盘可用约 5.9 GiB，/www 约 4.0 GiB；仅新上传两份约 64 MB 的成品。
- 配置由 64 条变为 65 条，完整历史及其他配置保留断言通过，发布和回滚 SQL 临时表演练通过。发布后前后端容器 ID、镜像和启动时间均不变；根盘约 5.9 GiB，/www 约 3.9 GiB。

## 2026-11-11 下载格式修正

- 原因是发布入口配置错误，专用工具已正确生成 ZIP；不需要修改或重新打包程序。Windows 更新链路本就支持下载、校验 ZIP，再解压并启动内部 EXE。
- 仅替换 Windows 6.3.2 条目的 `url_win`、`sha256` 两个字段，条目数、Mac、历史、文案、版本与升级规则不变，`system.app_config` 整行保持一致。
- 对实际已公网下载的 ZIP 调用正式 `verifySHA256` 和 `prepareInstaller`：校验成功，解压安装器 SHA256 与上表 EXE 一致，教程图片存在且非空；没有运行安装器。临时测试已清理，证据为 `D:/gh-pack/windows-632-20261111/zip-updater-verification.log`。
- 默认公网 `/api/system/local-agent-updates` 与 `/download` 均已核验指向 ZIP，无需通过额外查询参数绕过缓存。
- 修正前完整配置、精确回滚 SQL、临时表发布及回滚演练证据：`/opt/goodhr-release/windows-632-zip-correction-20261111/`。
