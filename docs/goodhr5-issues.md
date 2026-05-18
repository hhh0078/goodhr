# GoodHR 5 问题清单

> 2026-05-19 | 代码审查发现

## 🔴 严重

### 1. user 无 id，Agent 绑定传空
Go `auth.go` Login/Me 返回 user 只有 `{email}` 无 `id`。前端 `useAgent.bind()` 传 `cloud_user_id: user.id` 结果为 `undefined`，Local Agent 绑定存空 user_id。
**修复**：Login 时 upsert users 表，返回 user_id。

### 2. AI 筛选无 API Key
`task_executor.go:callAI()` 请求 `ai.58it.cn` 未设 `Authorization` 头。API Key 在 `system_ai_configs` 表但未读取。
**修复**：executeTask 读取 EffectiveConfig，传入 callAI。

### 3. 无平台登录状态检测
打开推荐页后不检查是否已登录。cookie 过期时页面在登录页，提取无候选人，任务"成功"但无实际效果。
**修复**：openPage 后检查页面内容/URL，未登录时通知用户。

## 🟠 中等

### 4. 任务执行状态不更新
executeTask goroutine 不更新 task.status(scanned/greeted/skipped 计数)，前端看不到实时状态。
**修复**：执行开始设 running，结束设 done/failed，递增计数。

### 5. 任务并发执行无保护
重复点击"运行"启动多个 goroutine 操作同一浏览器实例。
**修复**：检查 task.Status 是否已完成，是则拒绝。

### 6. 前端 profile/账号管理缺失
Local Agent 有 profiles API，云端有 platform-accounts API，但前端无账号管理 UI。任务"账号"下拉始终为空。
**修复**：前端添加 profile 管理面板。

### 7. defer stopBrowser 不可靠
Go 进程 kill 时 defer 不执行，浏览器进程残留。
**修复**：在 Run 末尾显式调用。

## 🟡 轻微

### 8. position map 类型转换
`[]string` → `map[string]any` → `[]any` 需 `toStringSlice()`。

### 9. scrollPage max_scrolls 边界
`MatchLimit/5` 为 0 时强制设 5，MatchLimit=0 时可能滚动过多。

### 10. AI prompt 只用岗位名
`positionDescription()` 仅返回 name，未使用 description 字段。

### 11. card_selector 无异常时静默失败
提取候选人空数组时任务继续执行无报错。

### 12. 用户注册流程依赖 DB
Login 不自动创建 users 记录，依赖管理员预填入或手动迁移。
