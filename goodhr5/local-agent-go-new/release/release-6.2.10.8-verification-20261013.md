<!-- 文件作用说明：记录详情滚动修复、用户简历数量及 Windows 6.2.10.8 可选更新的测试、生产备份、部署和回退证据。 -->

# Windows 6.2.10.8 详情滚动修复与用户简历数量

源码提交：`1024f45555be4a79b24870824336ecce08aa2bde`，已合并到 `origin/main`。

## 最终行为与原因

- 原日志封装只嵌入 `model.Runtime`，没有保留平台额外实现的 `model.DetailBrowser`。流程中的类型判断失败，导致 DOM 详情浏览直接跳过。现在由工厂保留原平台已有的能力，准确转发参数、错误和异常；不支持详情浏览的平台仍保持原行为。
- 岗位日志展示“滚动浏览详情”的开始、完成、失败及耗时；普通鼠标调用继续隐藏。滚动失败停止该候选人的正文读取，不把固定滚动次数描述成“已到底”。OCR 和 AI 看图沿用原截图滚动路径。
- 用户管理桌面表格增加数字“简历数”列，手机卡片也展示数量。PostgreSQL 按 `candidate_profiles.created_by_user_id` 批量计数，无逐用户查询；内置示例、无创建人记录不计入用户数量。接口只返回数量，不返回简历内容。
- 本次没有数据库结构或迁移变化，没有新增页面脚本注入。当前镜像、新源码和正式库的 100 条迁移逐字节及清单一致。

## 验证范围

- 本地 Go 全套：707 通过、14 按环境条件跳过；`go vet ./...` 通过。
- 云端 Go 全套：524 通过、53 按环境条件跳过；`go vet ./...` 通过。随后仅修正 PostgreSQL 测试数据的来源唯一键和合法订阅日期。
- 真实 PostgreSQL 集成验收通过：合成用户数量 `2/1/0`、示例排除、无创建人排除、分页和搜索。临时测试库已删除；证据 `/opt/goodhr-release/detail-scroll-pg-test-20261013-3/test-output.private.log`。
- Browser Worker：207 通过、1 跳过；TypeScript 编译通过。
- 前端：34 项单元测试、TypeScript、生产构建以及用户管理桌面/手机 2 项 UI 验收通过。
- 隔离真实 Camoufox 滚轮验收通过：本机静态长详情首行 Y 从 `76.0` 移至 `-1907.2`，实际移动 `1983.2` 像素，详情浏览方法及日志返回成功。只使用标准 Locator 和真实鼠标滚轮。
- 部署前独立内存候选服务完成真实 HTTP 数量 `0 → 1 → 0` 验收，无正式数据库、Redis、SMTP、账号或文件挂载。
- 本次未在真实猎聘账号上运行招聘任务。临时启动安装包程序的额外验收被自动审批拒绝（`blocked by policy`），未执行该项；安装器编译、版本和二进制来源核验均完成。

## Windows 公开产物

- 下载：<https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.8-windows-x64-5afe2f4f.zip>。
- ZIP：`30,692,354` 字节，SHA256 `5afe2f4f92ecb3c1665faf1e4c505153075a74ebedff4153308f6817e16d1918`。
- 安装器：`31,150,259` 字节，SHA256 `b9422748a9c58db406c528fc2a7609d2680d34544ee2c8a228120146b73cba0a`，`ProductVersion=6.2.10.8`。
- Agent SHA256：`e0855410dc463fb659dab4d74cdddf32eb6e6296ca4497b91e236e21eeaad562`。
- `go version -m`：`vcs.revision=1024f45555be4a79b24870824336ecce08aa2bde`，`vcs.modified=false`。
- 短目录干净源码构建成功。生产 `build-info.json` 的网站为 `https://goodhr.58it.cn`。
- ZIP 平铺包含安装器、安装教程图片、安装说明；CRC 与嵌入安装器摘要通过。安装说明为打开压缩包双击运行，无需解压。
- OSS2 服务器文件、本机完整公网回读和服务器完整公网回读大小与摘要均一致。

## 可选更新与生产数据保护

- Windows 最新 `6.2.10.8`，Mac 最新仍为 `6.2.10.7`；新 Windows 条目没有 Mac URL。
- `system.app_config.local_agent_version` 仍为 `6.2.10.1`，配置全行未改；前端版本判断验证 `6.2.10.1/6/7 → 6.2.10.8` 均为可忽略补丁。
- 历史列表 `43 → 44`；只在前方增加本次 Windows 记录，原 43 条逐项完全保留，其他配置字段保持原值。
- 全库同一导出快照备份 `100,131,998` 字节，SHA256 `66cb7b467ccceaabbb8b5b8214261fcb1a86f4b2332368756f1392d0f2bbcb2e`。
- 私有证据目录：`/opt/goodhr-release/windows-optional-6.2.10.8-1024f455`（目录 0700、备份/配置文件 0600）。
- 在独立无网络 PostgreSQL 容器、`/opt` 私有数据目录恢复，47 张表结构与全部行数、100 条迁移、配置全字段一致。CAS 发布与精确回退在恢复库演练通过并最终 ROLLBACK；演练容器已精确清理。
- 正式发布使用两项配置的全行快照和行锁 CAS，只更新 onboarding 的新版本条目和时间。
- 配置回退 SQL：`/opt/goodhr-release/windows-optional-6.2.10.8-1024f455/plan/rollback.sql`，SHA256 `8a58aa9843a21f0738091796df04a12ba141441496821bd7d47e01bd754c55b1`；只删除本次精确记录，保留后续更新和全部 Mac 记录。

## 云端部署与回退

- 生产主机 `ldclouda31539` / `43.228.79.108`。
- 后端容器 `5598824e483ce4c64f1f5870c29a819f2fb20bcf27e4cdc6503737b4ad94798d`，镜像 `sha256:a675e52fa75af2ed11717437f6a489d3f4f73f0e4ff464b92e773b9fb7fc8b95`。
- 前端容器 `02a0d0f98929811a2d3577d6f97300203cc8069a09cdd61d07c8226d9e47e766`，镜像 `sha256:f75a0b99a5d86edc62763281f8a154a0d7a98174b7ddbfa150ead939a9b80a66`。
- 两项源提交与发布源码一致，运行且重启 0 次，环境、命令、端口、网络和实际挂载均与原服务核对。
- 旧两容器及完整配置保留为 `*-backup-detail-scroll-resume-count-20261013-1024f455-r3`。
- 发布证据：`/opt/goodhr-release/detail-scroll-resume-count-20261013-1024f455-r3`。回退入口：`python3 <该目录>/cloud-resume-count-release.py rollback --root <该目录>`。
- 早先发布检查误用开发端口 8084，自动恢复原服务；已按原运行配置解析后端 `127.0.0.1:8086` 和前端 `127.0.0.1:5175`，重新独立准备并成功发布。失败容器和日志保留。
- 公网用户管理 JS `28-hkkg5wex77.js` 与正式镜像摘要一致（`159714845defe72a9cfe2163f2892c8bb47d50696dafd004cc923c27a2c78166`），包含“简历数”和 `resume_count`。
- 公网首页、下载页、`llms.txt`、更新配置、插件配置均 HTTP 200；本机及公网用户管理、候选人、支付管理 API 在未登录时均 HTTP 401。
- 下载页 HTML 已包含本次 Windows ZIP；本机独立复核 Windows/Mac/强制门槛/历史条数和公开摘要全部通过。

本机完整测试、打包和公开验收证据：`D:\work\goodhr-release-artifacts\20261013-detail-scroll`。真实账号运行和 macOS 本地包不属于本次已完成的运行验收。
