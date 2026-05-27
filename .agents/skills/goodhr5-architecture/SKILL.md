---
name: goodhr5-architecture
description: GoodHR 5 项目架构规范。在 goodhr5 子目录下开发时自动生效，定义云端/本地职责边界、平台配置归属、Local Agent API 设计原则。
---

# GoodHR 5 架构规范

## 架构边界

GoodHR 5 是三部分组成的系统，职责必须严格分离：

| 组件 | 职责 | 禁止 |
|------|------|------|
| 云端 Go 后端 | 用户认证、配置管理、任务元信息、平台选择器配置、解析决策逻辑 | 不保存候选人详情、截图、OCR、cookie |
| 云端 Vue 前端 | 用户操作界面、任务控制台 | 不直接操控浏览器 |
| 本地 Python Agent | 浏览器控制、页面操作、截图、OCR、本地文件管理 | 不包含平台解析逻辑、不包含筛选决策 |

**关键原则：云端是大脑，本地是手脚。**

## Local Agent 设计原则

- **纯执行器**：只提供原子化浏览器操作 API
- **同步等待**：每一个执行操作都必须等待其返回结果，禁止 fire-and-forget
- **参数化**：URL、选择器、操作参数均由云端下发，Local Agent 不硬编码任何平台信息

### Local Agent API 风格

```
POST /api/v1/browser/start   — 启动浏览器
POST /api/v1/browser/stop    — 关闭浏览器
POST /api/v1/page/open       — 打开页面
POST /api/v1/page/scroll     — 滚动
POST /api/v1/page/extract    — 按选择器提取文本/属性
POST /api/v1/page/click      — 点击
POST /api/v1/page/screenshot — 截图
```

## 数据边界

- **云端不保存**：候选人详情、截图、OCR 文本、招聘平台 cookie/profile
- **本地只存在 agent_data/**：所有敏感数据仅限本地 `agent_data/` 目录
- **云端保存**：用户信息、配置、机器绑定、任务元信息、日志摘要

## 平台配置归属

平台选择器配置存储在云端 PostgreSQL `system_configs` 表：

- Key 格式：`platform.{平台ID}`
- Value 为完整 JSON 配置

Local Agent 不硬编码任何平台选择器。

## platform/base.py 的正确归属

| 内容 | 归属 |
|------|------|
| `PlatformConfig` 数据类 | 云端 Go 后端 |
| `CandidateInfo` 数据类 | 云端 Go 后端 |
| `BaseParser` 抽象类 | 云端 Go 后端 |
| `screenshot_detail` 截图方法 | 本地 `app/screenshot.py` |
| `_scroll_and_stitch` 滚动拼接 | 本地 `app/screenshot.py` |
| `_merge_two` 图片合并 | 本地 `app/screenshot.py` |
| `_images_are_same` 图片比较 | 本地 `app/screenshot.py` |
| `_fallback_screenshot` 兜底截图 | 本地 `app/screenshot.py` |
| `_compute_strip_diff` 像素差异 | 本地 `app/screenshot.py` |
| `click_box_random_point` 随机点击 | 本地 `app/humanize.py` |
| `navigate_to_recommend` 导航 | 参数化后留在本地 |
| `wait_for_cards` 等待卡片 | 参数化后留在本地 |

## 执行注意事项

- 每个 JS 执行操作必须使用 `await` 等待返回
- Local Agent API 使用 request-response 模式，不依赖 WebSocket
- 任务执行中的真实状态归属 Local Agent
- 操作失败时返回明确错误信息，供云端调度重试或跳过
