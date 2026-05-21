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
| 云端后端 | system_configs 迁移 | DONE | 0002 创建表，0003 插入 Boss + 智联平台选择器配置 |
| 云端后端 | 系统配置存储 | DONE | SystemConfigStore 接口 + 内存 + PostgreSQL 双实现 |
| 云端后端 | 平台配置解析器 | DONE | PlatformConfig 结构体、ParsePlatformConfig、ExtractFieldSelectors |
| 云端后端 | 平台配置 API | DONE | `GET /api/platforms/config/` 返回已启用的平台选择器配置 |
| 云端后端 | 任务编排器 | DONE | TaskExecutor 编排主流程：启动浏览器→打开页面→滚动→提取→处理候选人→打招呼 |
| 云端后端 | 关键词筛选模块 | DONE | KeywordFilter：支持与/或模式、排除词、概率通过 |
| 云端后端 | 任务执行路由 | DONE | `POST /api/tasks/{id}/run` 异步执行，注入系统配置/岗位/日志 |
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
| 本地 Agent | FastAPI 改造 | DONE | 从 http.server 升级为 FastAPI + uvicorn，版本号 0.2.0 |
| 本地 Agent | pyproject.toml 与依赖管理 | DONE | 包含 fastapi/uvicorn/playwright/cloakbrowser/Pillow/numpy/pydantic/httpx |
| 本地 Agent | 浏览器控制模块（browser.py） | DONE | 从 goodhrpy 迁移 CloakBrowser 封装，BrowserManager、持久化上下文、僵尸进程清理 |
| 本地 Agent | 人类行为模拟模块（humanize.py） | DONE | 从 goodhrpy 迁移随机延迟、仿真人滚动、打字、点击等辅助函数 |
| 本地 Agent | 平台解析器基类（platform/base.py） | DONE | 从 goodhrpy 迁移 CandidateInfo、PlatformConfig、BaseParser、弹框截图拼接 |
| 本地 Agent | 架构纠正：删除 platform 目录 | DONE | PlatformConfig/CandidateInfo/BaseParser 属云端，截图拼接移入 screenshot.py，点击移入 humanize.py |
| 本地 Agent | 弹框截图模块（screenshot.py） | DONE | 从 base.py 拆分出 screenshot_modal、stitch_screenshots 等参数化函数 |
| 本地 Agent | CloakBrowser 控制 | DONE | 启动、关闭、打开页面 |
| 本地 Agent | 页面基础操作 API | DONE | 查找、滚动、随机位置点击（humanize.py + base.py 点击方法） |
| 本地 Agent | 浏览器控制 API 注册 | DONE | POST browser/start、stop、status、page/open、scroll、extract、click、screenshot 全部同步等待 |
| 本地 Agent | Boss 平台执行能力迁移 | TODO | iframe 内查找、当前可见候选人 |
| 本地 Agent | 截图能力迁移 | DOING | 截图拼接逻辑已迁入 base.py，PaddleOCR 待迁移 |
| 本地 Agent | PaddleOCR 能力迁移 | DONE | 从 goodhrpy 迁移 ocr.py，懒加载、线程池异步、v3/v2 双 API 兼容 |
| 本地 Agent | OCR API 注册 | DONE | GET /api/v1/ocr/status、POST /api/v1/ocr/recognize（Base64 输入）|
| 本地 Agent | 批量候选人提取 | DONE | page/extract mode=batch + card_element，按统一元素定位协议批量提取每个卡片字段 |
| 本地 Agent | 任务 JSON 存储 | DONE | `POST /api/v1/tasks/init` 创建本地任务目录和 candidates.json |
| 本地 Agent | 截图/OCR 本地文件管理 | DONE | 截图列表/读取/删除和 OCR 文本写入接口已完成 |
| 协议 | 云端任务协议 | DOING | 云端任务元信息 API 已完成，Local Agent 执行协议待接入 |
| 协议 | Local Agent API 草案落地 | DOING | health/session/profile/browser/page/task（browser/page 路由待第二轮注册） |
| 协议 | 云端登录后初始化/绑定本地 Agent | DONE | 前端探测成功后绑定云端 `/api/agents/bind` 和本地 `bind-cloud-user` |
| 安全 | CORS/PNA | TODO | 允许正式云端域名访问 localhost |
| 安全 | 本地 token | TODO | 初始化后所有本地 API 携带 token |
| 发布 | 本地程序打包 | TODO | 首版先确认 macOS/Windows 目标 |
| 发布 | 版本检查与下载 | TODO | 云端管理下载链接 |

## 本次完成

- **TaskExecutor 完善**：接入关键词筛选 + 批量候选人提取。
  - `extractCandidates` 改用 Local Agent batch 模式（card_element + mode=batch）。
  - `processCandidates` 接入 `KeywordFilter`，按模式自动筛选并记录跳过/通过日志。
  - 新增 `candidateText` 和 `toStringSlice` 辅助函数。

- **云端任务编排器（task_executor.go）**：连接本地 Agent 的执行编排核心。
  - `TaskExecutor` 编排主流程：启动浏览器 → 打开页面 → 滚动 → 提取候选人 → 逐候选人筛选 → 打招呼。
  - `post()` 方法封装 Local Agent HTTP 调用，统一错误处理和 JSON 解析。
  - 支持 context 取消和步骤级错误处理。
  - 预留 AI 模式和关键词模式筛选入口。
  - Go 编译通过。

- **云端平台配置基础设施**：创建 system_configs 表和完整读写逻辑。
  - 迁移 `0002_add_system_configs.sql`，新增 `system_configs` 表（config_key、config_value JSONB、enabled）。
  - 创建 `SystemConfigStore` 接口 + `MemorySystemConfigStore` + `PostgresSystemConfigStore` 双实现。
  - 创建平台配置数据结构：`PlatformConfig`、`PlatformCard`、`PlatformActions`、`PlatformDetail` 等。
  - `PlatformCard.ExtractFieldSelectors()` 提取字段→选择器映射，供 Local Agent page/extract 调用。
  - 注册 `GET /api/platforms/config/` 返回已启用的平台选择器配置。
  - Go 编译通过。

- **迁移 PaddleOCR 模块（app/ocr.py）**：从 goodhrpy 迁入完整 OCR 能力。
  - 懒加载机制，首次调用时初始化 PaddleOCR 引擎。
  - 支持 PaddleOCR v3（predict）和旧版（ocr）双 API。
  - `ocr_image_async` 通过 `asyncio.to_thread` 在线程池执行，不阻塞事件循环。
  - 提供 `is_available()` 检测和 `close_ocr()` 内存释放。
  - 注册 `GET /api/v1/ocr/status` + `POST /api/v1/ocr/recognize`（接收 Base64 图片）。
  - 安装 paddlepaddle + paddleocr 依赖，更新 pyproject.toml。

- **注册浏览器控制 API**：在 Local Agent FastAPI 中注册 8 个浏览器操作路由。
  - `POST /api/v1/browser/start` — 启动 CloakBrowser（支持持久化、无头、代理配置）
  - `POST /api/v1/browser/stop` — 关闭浏览器，清理所有残留进程
  - `GET /api/v1/browser/status` — 查询浏览器运行状态
  - `POST /api/v1/page/open` — 打开指定 URL 页面
  - `POST /api/v1/page/scroll` — 仿真人滚动加载列表
  - `POST /api/v1/page/extract` — 按选择器映射提取文本内容
  - `POST /api/v1/page/click` — 带延迟的元素点击
  - `POST /api/v1/page/screenshot` — 弹框截图（支持滚动拼接）
  - 每个操作均同步等待完成后返回结果，浏览器未启动时返回明确错误。

- **架构纠正**：删除误放在 Local Agent 的 `app/platform/` 目录。
  - `PlatformConfig`、`CandidateInfo`、`BaseParser` 属云端决策层，已移除。
  - 弹框截图拼接逻辑移至新文件 `app/screenshot.py`。
  - 随机点击 `click_box_random_point` 移至 `app/humanize.py`。
  - 导航和等待元素改为参数化函数 `navigate_to_page`、`wait_for_elements`。
  - 更新全局 skill 和项目 skill（`goodhr5-architecture`）记录架构边界。

- **创建项目 skill `goodhr5-architecture`**：放在 `goodHR/.agents/skills/` 下。
  - 明确云端/本地职责边界。
  - 定义 Local Agent API 设计原则（纯执行器、同步等待、参数化）。
  - 记录 `platform/base.py` 正确归属拆分表。

- **Local Agent 改造为 FastAPI**：从标准库 `http.server` 升级为 FastAPI + uvicorn。
  - 所有现有路由（health、session、profiles、tasks、screenshots、ocr）保持不变。
  - 新增全局异常处理器（FileNotFoundError → 404，ValueError → 400）。
  - 截图接口改用 `FileResponse` 返回图片。
  - 版本号升至 0.2.0。

- **创建 pyproject.toml**：Local Agent 依赖管理规范化。
  - 包含 fastapi、uvicorn、cloakbrowser、playwright、Pillow、numpy、pydantic、httpx。
  - 配置 ruff 代码检查规则。
  - pip 已配置阿里云镜像源，安装依赖顺利。

- **迁移浏览器控制模块（app/browser.py）**：从 goodhrpy 迁入 CloakBrowser 封装。
  - `create_browser` / `create_persistent_browser` 浏览器创建函数。
  - `BrowserManager` 浏览器生命周期管理器。
  - profile 锁文件清理、僵尸 Chromium 进程终止等辅助函数。
  - 配置参数化（解耦 goodhrpy 的全局 config 对象）。

- **迁移人类行为模拟模块（app/humanize.py）**：从 goodhrpy 迁入仿真人操作函数。
  - `random_delay` 随机延迟。
  - `human_scroll` / `scroll_to_load` 仿真人滚动。
  - `human_type` 仿真人打字。
  - `wait_and_click` 带延迟的元素点击。

- **迁移平台解析器基类（app/platform/base.py）**：从 goodhrpy 迁入平台抽象层。
  - `CandidateInfo` 候选人信息数据类。
  - `PlatformConfig` 平台 CSS 选择器配置。
  - `BaseParser` 抽象基类（候选人提取、打招呼、详情页操作、弹框截图拼接）。
  - 弹框滚动截图拼接算法（`_scroll_and_stitch`、`_merge_two`）。

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

1. 云端 Go 后端：插入各平台选择器配置数据（Boss/智联/猎聘）到 system_configs 表。
2. 云端 Go 后端：任务执行路由（POST /api/tasks/{id}/run）接入 TaskExecutor。
3. 云端 Go 后端：AI 筛选模块实现（调用 AI API）。
4. 云端 Vue 前端：任务运行监控面板，实时显示执行状态和日志。
5. 端到端联调：本地 Agent + 云端后端 + 前端完整流程测试。
