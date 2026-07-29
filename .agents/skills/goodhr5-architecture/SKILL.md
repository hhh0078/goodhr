---
name: goodhr5-architecture
description: GoodHR 5 项目架构规范。在 goodhr5 子目录开发云端 Go、Next.js 前端、本地 Go Agent 或 TypeScript Browser Worker 时使用，约束云端/本地职责、平台配置归属和浏览器自动化边界。
---

# GoodHR 5 架构规范

## 核心链路

保持唯一浏览器执行链路：

```text
云端 Next.js 控制台
  -> 本地 Go Agent
  -> Go 平台适配
  -> Go Browser Client
  -> TypeScript Browser Worker 封装能力
  -> TypeScript 原子能力
  -> CloakBrowser
```

禁止增加 Go 直连 CDP、第二个 Worker 或前端直控浏览器。

## 职责边界

| 组件 | 负责 | 禁止 |
|---|---|---|
| 云端 Go 后端 | 登录、会员、余额、岗位、用户运行设置、状态、统计，以及用户开启结构化简历后的简历库数据 | 操控浏览器；保存 Cookie、Profile、截图或未开启结构化输出时的完整候选人详情 |
| 云端 Next.js 前端 | 用户界面、本地任务控制台 | 直接调用 CloakBrowser 或 Playwright |
| 本地 Go Agent | 启动前检查、任务流程、平台适配、AI/OCR、本地状态 | 直接调用 Playwright；把平台差异塞进公共流程 |
| TypeScript Worker | 与平台无关的查找、移动、点击、输入、滚轮、读取、截图和下载 | 包含平台名、业务流程、AI、数据库或云端调用 |

## 平台配置归属

- 平台 URL、页面行为和选择器只放在 `local-agent-go-new/internal/platform/{platform}/config.json`。
- 使用 `go:embed` 随本地程序发布；任务启动时直接加载本地配置。
- 本地程序不得从云端请求、合并或覆盖平台配置。
- 所有选择器继续使用统一强类型 `SelectorSpec`。
- Go 平台文件只引用选择器逻辑键，不在 Go、Worker、云端数据库迁移中重复写 CSS。

## 本地任务与浏览器边界

- Go 负责编排，Worker 只执行具体浏览器动作。
- Worker 原子能力不得暴露给 Go；Go 只能调用封装能力。
- 点击必须依次执行查找、移动、原子点击；输入必须依次执行查找、移动、聚焦、原子输入。
- 所有请求同步等待结果，禁止 fire-and-forget。
- 公共流程不得根据平台名写例外；平台差异放入 `internal/platform/{platform}`。

## 页面零脚本注入

- 严禁 `evaluate`、`$eval`、`$$eval`、`addScriptTag`、`addInitScript` 和 `dispatchEvent`。
- 页面读取和操作只能使用标准 `Page`、`Locator`、鼠标、键盘、真实滚轮和截图。
- 禁止通过 JavaScript 修改 DOM、滚动、焦点或页面状态。

## 数据边界

- 云端不保存招聘平台 Cookie、Profile、截图或原始 OCR 文件。
- 只有岗位明确开启 `output_structured_resume` 时，AI 完整流式响应中的结构化简历才能异步同步到云端简历库。
- 主流程必须在 score、reason 等评分字段完整后立即继续，不能等待结构化简历字段；后台同步失败只记日志，不得回滚已经完成的页面动作。
- 浏览器 Profile、截图、下载和敏感临时数据只保存在本地数据目录。
- 未开启结构化简历时，云端只接收任务状态、累计统计和不含敏感详情的摘要。

## 修改前检查

- 先读取目标目录最近的 `AGENTS.md`；`local-agent-go-new/AGENTS.md` 是该目录的详细权威规范。
- 先搜索是否已有相同接口、方法、选择器逻辑键和错误码。
- 判断改动属于云端业务、本地公共流程、平台适配还是 Worker 动作，再放入对应目录。
