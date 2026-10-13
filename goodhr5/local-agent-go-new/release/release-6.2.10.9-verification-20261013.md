<!-- 文件作用说明：记录 Windows 6.2.10.9 组件原地安装修复的正式可选发布、产物校验、配置保护与回退依据。 -->

# Windows 6.2.10.9 组件安装修复

源码提交：`3701763b7104da3ca46b94dda080604d9f44cb25`，已合并到 `origin/main`。

## 修复与验证

- 用户测试截图中 Node 已安装成功，Camoufox 在解压完成后移动目录时出现 `Access is denied`；此前测试包仅处理了 Node。截图无法单独证明安全软件拦截。
- Node、Camoufox 和 OCR 统一在独立永久目录原地解压、验证并登记相对目录与可执行路径，不再移动或覆盖整个组件目录。
- 浏览器的 Worker、版本文件、中文语言包、字体、原生资源和 UBO 共用实际组件根目录；OCR 客户端通过运行组件管理器动态选择新路径。安装或版本记录保存失败保留旧组件与记录。
- Windows `go test ./... -count=1` 通过，包括真实 Node 安装、组件损坏与缺模型、目录迁移、重启、并发状态读取、目录/文件占用和版本文件占用回归。
- 实际 Windows 子文件占用复现 `rename ... Access is denied`，原地检查与登记通过；旧浏览器/OCR 目录被占用时完整安装通过。
- 正式包 Worker TypeScript 编译通过。真实 Camoufox 152.0.4-beta.30 样本完整经过下载、校验、解压、路径登记和资源检查，使用本包 Worker 启动及关闭无界面浏览器通过；使用隔离 Profile，没有访问招聘页面。
- 直接执行前端现有版本判断：`6.2.10.1/6/7/8 → 6.2.10.9` 均为可忽略补丁；旧前三段版本的升级规则保持原样。
- 此次按用户要求只发布 Windows。公共源代码保留 Mac .app 与 Vision OCR 适配；上一轮已完成 Mac 双架构交叉编译，尚未真机验证；本次没有重新编译或发布 Mac 安装包。

## 正式 Windows 产物

- 官网入口：<https://goodhr.58it.cn/download>。
- 自动更新与公开 ZIP：<https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.9-windows-x64-ea55f7c1.zip>。
- ZIP 大小 `30,689,948` 字节，SHA256 `ea55f7c1821731417e19c8965bdfb167108e2c9f4c9a76d7c5eb4bcd6d64d3e1`。
- 独立安装器：<https://oss2.58it.cn/GoodHR-Local-Agent-Setup-v6.2.10.9-windows-x64-7a754cc4.exe>。
- 安装器大小 `31,148,545` 字节，SHA256 `7a754cc4f99a33a865d448b05110f94dd98de6e17524201b0d22f917cf41c786`。
- Agent SHA256 `3c23c95c556a950a6fbc6287d02e9325ea5550fc3ab25a74be4ec343e65d2712`；包内版本 `6.2.10.9`、环境 `production`、站点 `https://goodhr.58it.cn`。
- 正式升级 ZIP 平铺且仅含安装器、安装教程图片与安装说明，CRC 和嵌入安装器摘要一致。脚本生成的便携目录 ZIP 未用于自动更新，避免更新器误把主程序当成安装器。
- 服务器文件及完整公网下载的大小、ZIP 摘要、全部 CRC 与安装器摘要一致；独立安装器公开 HEAD 返回 200 和正确大小。此前测试安装器与此次正式安装器逐字节一致。

## 可选更新与配置保护

- 正式主机 `ldclouda31539 / 43.228.79.108`；正式库 `goodhr5`，数据库 OID `25453`，系统标识 `7631134662026391594`。
- 仅向 `system.onboarding_config.local_agent` 前插一条 Windows `6.2.10.9`，新条目 `url_mac=""`；列表 `44 → 45`，原 44 条逐项完整保留。
- 全部 Mac 记录保持原样，Mac 最新仍为 `6.2.10.7`；没有改变 Mac 地址或校验值。
- `system.app_config` 全行保持原样，`local_agent_version` 仍为 `6.2.10.1`；其他 onboarding 字段、运行组件清单和平台开关均保持原样。
- 发布使用事务、行锁和两项配置值的并发比较检查；没有部署云端代码或重启服务。
- 公开更新 API 已返回 Windows 6.2.10.9 和正确 ZIP/SHA256；下载页同时包含新 Windows 地址和原 Mac 地址。

## 备份与回退

- 私有证据目录：`/opt/goodhr-release/windows-optional-6.2.10.9-3701763b`，目录权限 0700，备份和计划文件 0600。
- 完整正式库备份 `database-before.dump`：`100,307,897` 字节，SHA256 `4c2de2510f4ab7d701d8975d097b7a4f9e564fa6c9b55cb428f011b313107f7c`；备份目录读取校验通过。本次未在独立数据库恢复全库。
- 保存两项配置完整行的 `configs-before.json` 和 `configs-after.json`。
- `plan/publish.sql`、`plan/rollback.sql` 已在回滚事务中演练，演练结束后配置全行与原始快照相同。
- 回退 SQL：`/opt/goodhr-release/windows-optional-6.2.10.9-3701763b/plan/rollback.sql`，SHA256 `1cf00abcf39104828e01f5e92f518c77b50ce674c3a264c03c5370e6ebf755d8`。
- 回退只删除本次内容仍完全匹配的 Windows 记录，保留后续发布、Mac 和其他配置；记录已变动或重复时停止，不覆盖后续改动。
- 本机清单：`D:\gh-pack\runtimefix-3701763b\windows-release-manifest.json`；服务器包含 `prepared.json`、`published.json` 与完整下载校验证据。
