<!-- 文件作用说明：记录下载简历自动导入、消息中心、简历公开分享的交付范围、测试和正式发布证据。 -->

# 简历自动导入、消息中心与分享发布记录

2026-10-10 已发布到 https://goodhr.58it.cn 。本记录更新之前简历库验收文档中的待部署状态；招聘网站真实账号操作仍需单独验收。

## 交付行为

| 功能 | 实际行为 |
| --- | --- |
| 来源细分 | 来源筛选和标签区分 Boss插件、猎聘插件、智联插件、无忧插件；保留旧来源兼容，不新增四套插件包 |
| 下载自动导入 | 监听 GoodHR 打开的 Firefox；从四个平台下载的 PDF、DOC、DOCX 复用现有 AI 导入；明确识别为简历、姓名和手机号完整、未重复才自动入库 |
| 简历库设置 | 右上角设置弹框；“将下载的简历通过 AI 导入到 GoodHR”默认开启，按账号保存；保存后通知本地刷新，并每30秒同步 |
| 失败处理 | AI、文件、查重或入库需核对时保留本地草稿，发送站内消息，可从消息进入草稿；不自动重复 AI 请求或重写结果不明的入库请求 |
| 消息中心 | 右上角铃铛，浮层只列标题，展开正文即已读；每10秒检查摘要，慢请求结束后再计时，避免重叠；已读满72小时由后台定期删除，未读保留 |
| 消息复用 | `MessageService.Push` 供其他模块使用；账号计数和版本配合 Redis 缓存及同账号查询合并；Redis 不可用时有数据库和短时内存回退；永久摘要凭据避免清理后重推重复消息 |
| 简历分享 | 详情页和详情弹框复用资料组件；可生成免登录链接及二维码，公开页也显示二维码；可以取消分享；公开资料不包含内部备注、推荐记录、AI评分等内部数据 |

普通独立 Firefox 不在监听范围。旧下载、关闭期间下载、切号前任务不补扫。同账号重复文件按 SHA256 去重；原件和本地路径不上传云端。下载来源判断及具体边界见 [本地导入说明](../goodhr5/local-agent-go-new/docs/resume-download-import.md)。

## 验证

- 云端和本地程序 `go test ./...` 通过；云端集成使用真实 PostgreSQL、Redis，包含账号权限、来源筛选、消息并发、三天清理、幂等、缓存、分享及真实路由。
- Browser Worker 199 项、前端25项单测通过；TypeScript 检查通过。
- 前端 UI 共41项不同用例通过，覆盖简历、设置、消息、分享、个人 AI 配置和更新流程；公开页390px显示无横向溢出。另有定向复测。
- Windows 客户端编译及安装包构建成功；公开 ZIP 独立下载、CRC、三个文件内容及安装器 SHA256 校验通过。
- 在生产数据库备份的独立克隆中执行六份迁移，34,609份简历原始字段、13,386条事件、10条旧备注完整保留；正数工资及负数备注回退测试通过，小数等不兼容情况安全拒绝回退。
- 正式切换后健康检查通过；前后端运行、重启次数为0、未发现本次检查匹配的错误日志；96份迁移全部已执行。生产数量仍为34,609份简历、13,386条事件和10条备注。

未使用真实招聘账号逐一下载四个平台的简历；AI识别、失败恢复及页面交互使用测试替身和样本验证。Windows 环境没有可用 CGO 编译器，未执行 Go `-race`。

## 云端发布

- 功能提交：`b53592b383cf3c4f30504dba0617b4cee1ebb68a`。
- 合并到远程 `main` 的部署代码：`805fce9f8b848f1f5f46e09befa5a53c4959db50`；镜像 revision 标签与归档一致。本记录作为后续文档提交，不改变已部署代码。
- 已核对服务器：`ldclouda31539`，`43.228.79.108`。
- 后端镜像：`sha256:7c506d2925a6e7e2c73d0b1eaf1a19ea41161ef21e808fe1e325d2db964ffb42`。
- 前端镜像：`sha256:c813ad4f08100dc2ca4d1ed599689c4aa47d5e34193c044de30f9ff7ba86c846`。
- 本次迁移：`0084_candidate_library_upgrade.sql`、`0085_browser_extension_release_config.sql`、`0086_candidate_notes.sql`、`0087_user_messages.sql`、`0088_resume_library_settings.sql`、`0089_candidate_shares.sql`。已有同数字前缀迁移保留原文件和内容，执行记录按完整文件名区分。
- 先在隔离候选端口检查，再切换正式前后端；保留端口、挂载及网络配置。PostgreSQL、Redis 容器 ID、启动时间和镜像均未改变。
- 线上登录和简历页面 HTTP 200；无效分享页 HTTP 200并显示失效提示，公开查询返回404；未登录消息和设置接口返回401。分享页面禁止索引、不发送 Referrer、无百度统计脚本。

备份目录：`/www/wwwroot/goodhr-production/backups/resume-download-20261010-222300`。内含完整数据库 dump、私有原配置、镜像和迁移清单、候选与切换结果、客户端配置备份及有条件的回退脚本，目录和敏感文件仅 root 可读。

数据库备份为90,728,100字节，SHA256：`afc009d793c524e31134a16fad948f4de8f9c000d4e825b4c42d425d11cb9366`；已确认恢复目录有效。旧前后端容器已停止并保留为 `goodhr-production-backend-backup-20261010-222300`、`goodhr-production-frontend-backup-20261010-222300`。

旧后端不支持新增的小数工资和新版备注：回退必须先运行该目录内兼容检查；发现不兼容数据时拒绝自动切回旧后端，避免数据丢失。不能只重启旧容器。

## Windows 客户端 6.2.10.3

下载监听需要升级本地程序。新版本为第四段补丁版本，沿用现有可选更新规则，历史39条更新保留，发布后共40条。

- [下载页面](https://goodhr.58it.cn/download)。
- [6.2.10.3 下载包](https://oss2.58it.cn/GoodHR-Local-Agent-v6.2.10.3-windows-x64-ad42fac9.zip)，30,663,705字节。
- ZIP SHA256：`ad42fac97d53dbe5d6b9e39f18ea2e466ef8598352ff2df914e4941cadef4f6a`。
- 安装器 SHA256：`ea01cd0580f2c9d37f6fe65c17fadc5eefd6ae8077e1e6d35d2f2206a6d1397f`。
- ZIP保留原安装教程图片；DOC解析依赖许可证随安装包交付。
- 客户端发布配置采用事务及并发检查，仅增加本次版本；回退在事务内预演通过；公开更新接口和下载页面验证通过。此次未发布新的 macOS 安装包。

完整发布证据在服务器备份目录，包含 `production-final-verification.json`、`production-data-counts.json`、`deployment-result.json`、`migration-preflight-result.json`、`rollback-clone-verification-result.json`、`client-release.log` 和 `local-agent-public-verification.json`。
