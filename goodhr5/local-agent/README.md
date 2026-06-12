# GoodHR 5 Local Agent

本地 Agent 运行在用户电脑上，负责：

- CloakBrowser 控制（browser.py）。
- 招聘平台页面查找、滚动和点击（humanize.py + platform/base.py）。
- 当前可见候选人提取（platform/ 各平台解析器）。
- 详情弹框截图（platform/base.py 弹框截图拼接）。
- AI 图片识别。
- 本地任务 JSON、截图和图片识别文本文件管理。

当前版本已升级为 FastAPI（v0.2.0），使用 uvicorn 启动。

## 启动

```bash
# 激活虚拟环境
source .venv/bin/activate

# 启动 Local Agent
python3 -m app.main
```

## 桌面启动器

开发环境可直接运行桌面启动器：

```bash
python3 launcher.py
```

启动器会自动：

- 创建运行数据目录。
- 清空窗口中的本次运行日志。
- 启动 Local Agent 服务。
- 在窗口中显示当前状态和日志。
- 提供打开官网、停止服务、清除日志、重新启动按钮。

默认运行数据目录：

```text
macOS: ~/Library/Application Support/GoodHR/
Windows: %APPDATA%/GoodHR/
```

目录内保存：

```text
agent_data/   本地任务、机器码、绑定信息
cookies/      浏览器登录 profile
config/       配置文件
vendor/       CloakBrowser 运行文件
```

打包产物不再内置 CloakBrowser。首次启动时，程序会请求官网公开 JSON 获取下载地址，自动下载并解压到运行数据目录：

```text
https://goodhr5.58it.cn/agent-browser-downloads.json
```

macOS 打包：

```bash
sh packaging/build_mac.sh
```

Windows 打包请在 Windows 机器上执行：

```powershell
powershell -ExecutionPolicy Bypass -File packaging\build_windows.ps1
```

默认从 `127.0.0.1:95271` 到 `127.0.0.1:95279` 自动尝试，遇到端口占用会继续尝试下一个端口。

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

`machine_id` 优先由系统稳定硬件 UUID 哈希生成：macOS 使用 `IOPlatformUUID`，Windows 使用系统硬件 UUID。重装 Local Agent 后，同一台电脑会得到相同机器码；硬件 UUID 不可用时，会用系统类型、主机名、用户目录等信息兜底。本地只保存哈希后的机器码，不保存原始硬件 UUID。

本地程序不再绑定云端账号，也不保存登录 token。

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
ocr/        兼容旧路径，用于保存图片识别文本
```

`candidates.json` 里除候选人列表外，还会保存任务创建时同步下来的岗位模板快照。

候选人详情、图片识别文本和任务岗位模板快照只写入本地任务目录；截图仅作为内存中的中间数据使用，不长期保存到本地。

## 截图/识别文本文件管理

当前已提供：

```http
GET /api/v1/tasks/{task_id}/screenshots
GET /api/v1/tasks/{task_id}/screenshots/{filename}
DELETE /api/v1/tasks/{task_id}/screenshots/{filename}
POST /api/v1/tasks/{task_id}/ocr
```

当前版本默认不再长期保存截图文件；截图接口仅保留兼容能力。

图片识别文本写入：

```text
local-agent/agent_data/tasks/{task_id}/ocr/{candidate_id}.txt
```
