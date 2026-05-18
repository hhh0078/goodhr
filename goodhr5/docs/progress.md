# GoodHR 5 任务进度表

更新时间：2026-05-18

## 状态说明

- `TODO`：未开始
- `DOING`：进行中
- `DONE`：已完成并提交
- `BLOCKED`：被外部信息或决策阻塞

## 总进度

| 模块 | 功能 | 状态 | 备注 |
| --- | --- | --- | --- |
| 工程 | 创建 goodhr5 目录结构 | DONE | 云端后端、云端前端、本地 Agent、文档目录 |
| 工程 | 初始化进度表 | DONE | 后续每个功能完成后更新本文件 |
| 工程 | 开发规范文档 | DONE | 模块化、文件头注释、方法中文注释、调用点说明 |
| 云端后端 | Go API 骨架 | DONE | 提供 `/health` 起步接口 |
| 云端后端 | 邮箱验证码登录 | DONE | 4 位验证码、校验、临时 token、SMTP 发信接口 |
| 云端后端 | PostgreSQL schema | DONE | 初始 SQL 迁移包含用户、Agent、平台账号、岗位、AI 配置、任务、日志 |
| 云端后端 | 岗位配置 API | DONE | `GET/POST/DELETE /api/positions`，支持关键词和默认问候语配置 |
| 云端后端 | PostgreSQL 岗位配置存储 | DONE | 配置 `GOODHR_PG_DSN` 后，PositionStore 切换到 PostgreSQL |
| 云端后端 | Redis 会话与验证码 | DONE | 配置 `GOODHR_REDIS_ADDR` 后使用 Redis；未配置时使用内存存储 |
| 云端后端 | 机器绑定 API | DONE | `POST /api/agents/bind`、`GET /api/agents/current`，当前使用内存 store |
| 云端后端 | PostgreSQL 机器绑定存储 | DONE | 配置 `GOODHR_PG_DSN` 后，AgentStore 切换到 PostgreSQL |
| 云端后端 | 系统/用户 AI 配置 | DONE | 系统默认、用户自定义、最终生效配置 API；当前使用内存 store |
| 云端后端 | PostgreSQL AI 配置存储 | DONE | 配置 `GOODHR_PG_DSN` 后，AIConfigStore 切换到 PostgreSQL |
| 云端后端 | 云端任务 API | DONE | `POST /api/tasks`、`GET /api/tasks`、`GET /api/tasks/{id}`，未配置 PostgreSQL 时使用内存 store |
| 云端后端 | PostgreSQL 平台账号与任务存储 | DONE | 配置 `GOODHR_PG_DSN` 后，PlatformAccountStore 和 TaskStore 切换到 PostgreSQL |
| 云端后端 | PostgreSQL 任务日志存储 | DONE | 配置 `GOODHR_PG_DSN` 后，TaskLogStore 切换到 PostgreSQL |
| 云端前端 | Vue 工程骨架 | DONE | 起步页面和 Local Agent 探测逻辑 |
| 云端前端 | 邮箱验证码登录页 | DONE | 接入 `send-code/login/me`，登录成功后探测本地 Agent |
| 云端前端 | 本地程序下载/启动提示 | DONE | 未检测到本地 Agent 时展示下载占位、启动步骤、重新检测 |
| 云端前端 | 岗位模板管理 | DONE | 接入岗位模板创建、列表、编辑回填、删除 |
| 云端前端 | 任务创建页面 | DONE | 任务创建已接入云端任务 API，并支持选择岗位模板 |
| 云端前端 | 任务列表与日志展开 | DONE | 任务卡片支持展开/收起日志，日志来自云端任务日志 API |
| 云端前端 | 本地候选人 JSON 管理 | DONE | 任务卡片支持读取、展示、删除本地候选人 JSON |
| 本地 Agent | Python Agent 骨架 | DONE | 提供 `/health` 起步接口 |
| 本地 Agent | 端口 9001-9009 自动监听 | DONE | 遇到占用自动尝试下一个端口，`/health` 返回实际端口 |
| 本地 Agent | 本地 machine_id | DONE | 写入 `agent_data/machine.json`，`/health` 返回机器码 |
| 本地 Agent | 云端账号绑定 | DONE | `POST /api/v1/session/bind-cloud-user` 写入 `cloud_account.json` |
| 本地 Agent | Profile/cookie 多账号管理 | DONE | 云端平台账号映射和本地 profile 元数据接口已完成；cookie 原文仍只在浏览器 profile |
| 本地 Agent | CloakBrowser 控制 | TODO | 启动、关闭、打开页面 |
| 本地 Agent | 页面基础操作 API | TODO | 查找、滚动、随机位置点击 |
| 本地 Agent | Boss 平台执行能力迁移 | TODO | iframe 内查找、当前可见候选人 |
| 本地 Agent | 截图能力迁移 | TODO | 复用当前可用的详情弹框截图代码 |
| 本地 Agent | OCR 能力迁移 | TODO | 复用当前 PaddleOCR 懒加载封装 |
| 本地 Agent | 任务 JSON 存储 | DONE | `POST /api/v1/tasks/init` 创建本地任务目录和 candidates.json |
| 本地 Agent | 截图/OCR 本地文件管理 | DONE | 截图列表/读取/删除和 OCR 文本写入接口已完成 |
| 协议 | 云端任务协议 | DOING | 云端任务元信息 API 已完成，Local Agent 执行协议待接入 |
| 协议 | Local Agent API 草案落地 | TODO | health/session/profile/browser/page/task |
| 协议 | 云端登录后初始化/绑定本地 Agent | DONE | 前端探测成功后绑定云端 `/api/agents/bind` 和本地 `bind-cloud-user` |
| 安全 | CORS/PNA | TODO | 允许正式云端域名访问 localhost |
| 安全 | 本地 token | TODO | 初始化后所有本地 API 携带 token |
| 发布 | 本地程序打包 | TODO | 首版先确认 macOS/Windows 目标 |
| 发布 | 版本检查与下载 | TODO | 云端管理下载链接 |

## 本次完成

- 任务创建表单新增岗位模板内容预览。
- 选中岗位模板后，可直接看到关键词、排除词、岗位描述和默认问候语。

- 候选人面板改为读取完整本地任务数据，而不是只读候选人数组。
- 修正前端本地候选人读取字段，改为匹配 Local Agent 实际返回的 `data` 结构。
- 候选人面板新增岗位模板快照展示，显示关键词、排除词和默认问候语。

- 本地任务初始化支持同步岗位模板快照。
- 前端创建任务后初始化本地任务时，会一并写入岗位名称、关键词、排除词和默认问候语。
- Local Agent README 补充本地任务目录中的岗位模板快照说明。

- 任务创建接口支持关联岗位模板 `position_id`。
- 前端任务创建表单新增岗位模板选择。
- 任务列表支持显示任务关联的岗位模板名称。
- 云端后端 README 增加任务接口 `position_id` 说明。

- 前端新增岗位模板管理面板。
- 接入云端岗位模板列表、保存、删除 API。
- 支持关键词、排除词、默认问候语和 AND/OR 模式编辑。
- 支持点击岗位模板回填表单后继续编辑。

- 云端后端新增岗位配置 API。
- 支持岗位配置创建、列表、删除。
- 岗位配置保存名称、关键词、排除词、描述、默认问候语和 AND/OR 模式。
- 配置 `GOODHR_PG_DSN` 后，岗位配置写入 PostgreSQL。
- 云端后端 README 增加岗位配置 API 说明。

- 云端后端新增 PostgreSQL AIConfigStore。
- 配置 `GOODHR_PG_DSN` 后，系统默认和用户自定义 AI 配置写入 PostgreSQL。
- 未初始化系统 AI 配置时，会自动写入一份默认配置。
- 云端后端 README 增加 AIConfigStore 的 PostgreSQL 说明。

- 云端后端新增 PostgreSQL AgentStore。
- 配置 `GOODHR_PG_DSN` 后，机器绑定写入 PostgreSQL。
- 机器绑定按用户和机器码 upsert，并返回最近活跃机器。
- 云端后端 README 增加 AgentStore 的 PostgreSQL 说明。

- 架构文档补充“任务运行态归属 Local Agent、网页只是控制台”的关键原则。
- 云端后端新增 PostgreSQL TaskLogStore。
- 配置 `GOODHR_PG_DSN` 后，任务日志摘要写入 PostgreSQL。
- 云端后端 README 增加 TaskLogStore 的 PostgreSQL 说明。

- 云端后端新增 `GOODHR_PG_DSN` 配置和 PostgreSQL 连接初始化。
- 平台账号映射新增 PostgreSQL store，支持创建、列表、删除。
- 任务新增 PostgreSQL store，支持创建、列表、详情读取。
- 任务创建时会校验平台账号是否属于当前登录用户。
- `NewServer` 改为返回错误，显式开启 PostgreSQL 时启动阶段就会校验连接。
- 云端后端 README 增加 PostgreSQL 启用说明。

- 前端新增 Local Agent API service，统一封装本地任务和候选人调用。
- 任务卡片新增“查看候选人”面板，按任务读取本地 `candidates.json`。
- 前端创建任务后会自动初始化对应的本地任务目录。
- 旧任务首次展开候选人时也会自动初始化本地任务目录。
- 候选人卡片支持显示名称、摘要、详情文本，并支持删除本地候选人记录。
- Local Agent 任务初始化改为幂等，重复调用不会覆盖已有候选人数据。

- Local Agent 新增截图文件列表、读取、删除接口。
- Local Agent 新增 OCR 文本写入接口。
- 截图读取和删除限制在当前任务 `screenshots/` 目录内。
- OCR 原文写入当前任务 `ocr/` 目录。

## 历史完成

- Local Agent 新增本地任务目录和候选人 JSON 管理模块。
- 新增任务初始化接口，每个任务创建独立目录。
- 新增候选人 JSON 读取、新增/更新、删除接口。
- 任务目录预留 `screenshots/` 和 `ocr/`，候选人详情仍只保存在本地。

- 前端任务卡片新增展开/收起日志。
- 展开任务时调用云端 `GET /api/tasks/{id}/logs`。
- 无日志时显示空状态。

- 云端后端新增任务日志模块。
- 新增 `GET /api/tasks/{id}/logs` 读取任务日志摘要。
- 新增 `POST /api/tasks/{id}/logs` 写入任务日志摘要。
- 日志只保存运行摘要，不保存候选人完整详情。

- 前端任务创建接入云端 `POST /api/tasks`。
- 前端任务列表接入云端 `GET /api/tasks`。
- 登录或恢复登录态后自动加载云端任务列表。
- 创建任务后自动刷新云端任务列表。

- 云端后端新增任务模块。
- 新增任务创建、任务列表、任务详情接口。
- 任务只保存平台、账号、模式、匹配上限、状态和统计摘要。
- 当前使用内存 `TaskStore`，后续替换为 PostgreSQL `task_runs` 表。

- 云端前端新增任务创建面板。
- 任务表单支持选择平台、平台账号、筛选模式、匹配上限。
- 读取云端平台账号映射，用于同平台多账号选择。
- 前端先创建任务草稿并展示扫描/打招呼/跳过/失败统计，后续接入云端任务 API。

- Local Agent 新增 profile 元数据管理模块。
- 新增本地 profile 列表、创建、删除接口。
- 本地 profile 元数据写入 `agent_data/profiles.json`。
- 当前只保存 profile 元数据，不保存 cookie 原文。

- 云端后端新增平台账号映射模块。
- 新增平台账号映射列表、创建、删除接口。
- 支持按 `platform_id` 过滤同一平台的多个账号/profile。
- 云端只保存显示名和 `local_profile_id`，不保存 cookie/profile 原文。

- 云端后端新增 AI 配置模块。
- 新增系统默认 AI 配置读取和更新接口。
- 新增用户自定义 AI 配置读取和更新接口。
- 新增最终生效 AI 配置接口，按“用户配置 > 系统默认配置”合并。
- 当前使用内存 `AIConfigStore`，后续替换为 PostgreSQL store。

- 云端前端在探测到本地 Agent 后自动初始化绑定。
- 调用云端 `POST /api/agents/bind` 保存机器绑定。
- 调用本地 `POST /api/v1/session/bind-cloud-user` 写入当前云端账号。
- 前端显示本地 Agent 绑定状态和绑定错误。

- 云端后端新增 Agent 机器绑定模块。
- 新增 `POST /api/agents/bind` 保存当前登录账号和机器码绑定。
- 新增 `GET /api/agents/current` 查询当前账号绑定机器。
- 当前使用内存 `AgentStore`，后续替换为 PostgreSQL store。

- 新增 PostgreSQL 初始迁移 `0001_initial_schema.sql`。
- 新增回滚脚本 `0001_initial_schema.down.sql`。
- 覆盖用户、Agent 机器绑定、平台账号映射、岗位、系统/用户 AI 配置、任务运行、任务日志。
- 明确云端 schema 不保存候选人详情、截图、OCR 原文和招聘平台 cookie/profile。

- 新增 `docs/development-standards.md`。
- 明确模块化、文件头用途注释、方法中文注释、调用点说明要求。
- 明确每完成一个功能必须更新进度表并单独提交。

- 云端前端增加邮箱验证码登录页面。
- 接入 `POST /api/auth/send-code`、`POST /api/auth/login`、`GET /api/auth/me`。
- 登录 token 保存到 `localStorage`。
- 页面改为登录云端后再探测本地 Agent。
- Go 后端增加基础 CORS，支持本地 Vite 页面调用 API。

- 云端认证增加 `Mailer` 发信接口。
- 配置 `GOODHR_SMTP_HOST`、`GOODHR_SMTP_USERNAME`、`GOODHR_SMTP_PASSWORD` 后，通过 SMTP 发送 4 位验证码。
- 未配置 SMTP 时使用开发模式 mailer，并返回 `debug_code` 方便本地联调。
- README 增加 163 SMTP 环境变量说明。

- 云端认证增加 Redis 版 `AuthStore`。
- 配置 `GOODHR_REDIS_ADDR` 后，验证码和会话写入 Redis。
- 未配置 Redis 时继续使用内存存储，方便本地开发。
- 新增云端后端 README，记录 Redis 环境变量和 key 规则。

- 云端认证增加 `AuthStore` 存储接口。
- 当前默认使用 `MemoryAuthStore`，验证码和会话逻辑不再直接依赖内存 map。
- 登录成功后保存会话，并新增 `GET /api/auth/me` 验证登录态。

- 云端前端在未检测到本地 Agent 时显示下载/启动提示。
- 保留重新检测按钮。
- 下载链接暂用占位，后续由版本发布模块接真实下载地址。

- 本地 Agent 增加 `POST /api/v1/session/bind-cloud-user`。
- 绑定信息保存到 `agent_data/cloud_account.json`。
- `/health` 返回 `bound_cloud_user_id`，方便云端页面识别本地绑定状态。

- 云端 Go API 增加 `POST /api/auth/send-code`。
- 云端 Go API 增加 `POST /api/auth/login`。
- 验证码先用内存 TTL 存储，并返回 `debug_code` 方便本地开发验证。
- 登录成功返回临时 Bearer token。

- Vue 页面加载后自动探测 `127.0.0.1:9001-9009` 的本地 Agent。
- 保留手动“检测本地程序”按钮，方便用户启动本地程序后重新检测。

- 本地 Agent 首次启动生成 `agent_data/machine.json`。
- `machine_id` 使用机器信息和随机 `install_id` 哈希生成。
- `/health` 返回本地 `machine_id`。

- 本地 Agent 支持 `9001-9009` 自动选端口。
- `/health` 返回实际监听端口。
- `GOODHR_AGENT_PORT` 可作为优先尝试端口。

- 创建 `goodhr5` 工程目录。
- 添加云端 Go 后端最小健康检查服务。
- 添加 Vue 前端最小页面，包含本地 Agent 端口探测按钮。
- 添加 Python 本地 Agent 最小健康检查服务。
- 添加本进度表。

## 下一步建议

1. 迁移现有截图 / OCR 执行逻辑到 goodhr5 的 Local Agent。
2. 开始落地 CloakBrowser 控制和页面基础操作 API。
3. 开始抽象 Boss / 智联平台执行入口，准备承接真实自动化流程。
