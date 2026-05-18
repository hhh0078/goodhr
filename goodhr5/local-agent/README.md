# GoodHR 5 Local Agent

本地 Agent 运行在用户电脑上，后续负责：

- CloakBrowser 控制。
- 招聘平台页面查找、滚动和点击。
- 当前可见候选人提取。
- 详情弹框截图。
- OCR。
- 本地任务 JSON、截图和 OCR 文件管理。

当前版本只提供 `/health`，用于云端页面探测。

## 启动

```bash
python3 -m app.main
```

默认从 `127.0.0.1:9001` 到 `127.0.0.1:9009` 自动尝试，遇到端口占用会继续尝试下一个端口。

如果设置了 `GOODHR_AGENT_PORT`，会优先尝试该端口，然后继续尝试默认端口范围。

## 本地数据

默认数据目录：

```text
local-agent/agent_data/
```

首次启动会生成：

```text
local-agent/agent_data/machine.json
```

`machine_id` 由系统类型、主机名、用户目录、安装 ID 等信息哈希生成。本地只保存哈希后的机器码和随机安装 ID，不保存用于上传的明文硬件信息。

绑定云端账号后会生成：

```text
local-agent/agent_data/cloud_account.json
```

本文件保存当前绑定的云端用户 ID、邮箱、本地调用 token 和绑定时间。

## Profile 管理

当前已提供：

```http
GET /api/v1/profiles
GET /api/v1/profiles?platform_id=boss
POST /api/v1/profiles
DELETE /api/v1/profiles/{profile_id}
```

本地 profile 元数据保存到：

```text
local-agent/agent_data/profiles.json
```

当前只保存 `platform_id`、`display_name`、`id` 等元数据，不保存 cookie 原文。

## 本地任务和候选人 JSON

当前已提供：

```http
POST /api/v1/tasks/init
GET /api/v1/tasks/{task_id}/candidates
POST /api/v1/tasks/{task_id}/candidates
DELETE /api/v1/tasks/{task_id}/candidates/{candidate_id}
```

每个任务一个目录：

```text
local-agent/agent_data/tasks/{task_id}/
```

目录内包含：

```text
candidates.json
logs.jsonl
screenshots/
ocr/
```

`candidates.json` 里除候选人列表外，还会保存任务创建时同步下来的岗位模板快照。

候选人详情、截图路径、OCR 文本和任务岗位模板快照都只写入本地任务目录。

## 截图/OCR 文件管理

当前已提供：

```http
GET /api/v1/tasks/{task_id}/screenshots
GET /api/v1/tasks/{task_id}/screenshots/{filename}
DELETE /api/v1/tasks/{task_id}/screenshots/{filename}
POST /api/v1/tasks/{task_id}/ocr
```

截图文件只允许读取和删除当前任务 `screenshots/` 目录内的文件。

OCR 文本写入：

```text
local-agent/agent_data/tasks/{task_id}/ocr/{candidate_id}.txt
```
