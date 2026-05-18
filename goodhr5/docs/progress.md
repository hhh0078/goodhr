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
| 云端后端 | Redis 会话与验证码 | DONE | 配置 `GOODHR_REDIS_ADDR` 后使用 Redis；未配置时使用内存存储 |
| 云端后端 | 机器绑定 API | DONE | `POST /api/agents/bind`、`GET /api/agents/current`，当前使用内存 store |
| 云端后端 | 系统/用户 AI 配置 | DONE | 系统默认、用户自定义、最终生效配置 API；当前使用内存 store |
| 云端前端 | Vue 工程骨架 | DONE | 起步页面和 Local Agent 探测逻辑 |
| 云端前端 | 邮箱验证码登录页 | DONE | 接入 `send-code/login/me`，登录成功后探测本地 Agent |
| 云端前端 | 本地程序下载/启动提示 | DONE | 未检测到本地 Agent 时展示下载占位、启动步骤、重新检测 |
| 云端前端 | 任务创建页面 | DOING | 前端任务草稿创建已完成，云端任务 API 待接入 |
| 云端前端 | 任务列表与日志展开 | TODO | 扫描总数、已打招呼、跳过、失败 |
| 云端前端 | 本地候选人 JSON 管理 | TODO | 通过 Local Agent 读取和渲染 |
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
| 本地 Agent | 任务 JSON 存储 | TODO | 每个任务一个目录和 candidates.json |
| 本地 Agent | 截图/OCR 本地文件管理 | TODO | screenshots/ 和 ocr/ |
| 协议 | 云端任务协议 | TODO | Vue 协调云端任务状态和 Local Agent |
| 协议 | Local Agent API 草案落地 | TODO | health/session/profile/browser/page/task |
| 协议 | 云端登录后初始化/绑定本地 Agent | DONE | 前端探测成功后绑定云端 `/api/agents/bind` 和本地 `bind-cloud-user` |
| 安全 | CORS/PNA | TODO | 允许正式云端域名访问 localhost |
| 安全 | 本地 token | TODO | 初始化后所有本地 API 携带 token |
| 发布 | 本地程序打包 | TODO | 首版先确认 macOS/Windows 目标 |
| 发布 | 版本检查与下载 | TODO | 云端管理下载链接 |

## 本次完成

- 云端前端新增任务创建面板。
- 任务表单支持选择平台、平台账号、筛选模式、匹配上限。
- 读取云端平台账号映射，用于同平台多账号选择。
- 前端先创建任务草稿并展示扫描/打招呼/跳过/失败统计，后续接入云端任务 API。

## 历史完成

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

1. 添加 PostgreSQL store 接口。
2. 添加云端任务 API。
3. 添加任务运行列表与日志展开。
