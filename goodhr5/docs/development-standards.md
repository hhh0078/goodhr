# GoodHR 5 开发规范

## 目标

本规范用于约束 GoodHR 5 后续所有代码开发，避免代码越来越难维护。

## 模块化

- 每个模块只负责一个清晰职责。
- 云端后端按 `cmd`、`internal/httpapi`、`internal/store`、`internal/config` 等方向拆分。
- 云端前端按页面、组件、服务、状态管理拆分。
- 本地 Agent 按路径、机器码、会话、浏览器、平台、任务数据、截图、OCR 拆分。
- 不允许把新业务继续堆进单个大文件。
- 新增功能时优先新增小模块，再在入口处做清晰组合。

## 文件头注释

每个新增代码文件顶部必须写明文件用途。

Go 示例：

```go
// Package httpapi 提供云端 HTTP API 的路由和处理器。
package httpapi
```

Python 示例：

```python
"""本文件负责管理 Local Agent 的本地机器码。"""
```

JavaScript 示例：

```js
// 本文件负责 GoodHR 云端首页的登录和本地 Agent 探测。
```

SQL 示例：

```sql
-- 本文件定义 GoodHR 5 云端数据库初始表结构。
```

## 方法注释

每个导出的函数、类方法、业务方法都必须有标准中文注释。

Go 示例：

```go
// NewServer 创建云端 HTTP 服务实例，并完成依赖注入。
func NewServer() *Server {}
```

Python 示例：

```python
def load_machine() -> dict[str, str]:
    """读取或创建本地机器码配置。"""
```

JavaScript 示例：

```js
// sendCode 调用云端接口发送邮箱验证码。
async function sendCode() {}
```

## 调用点说明

每个调用业务方法的地方，都要用简短注释说明“为什么调用这个方法”或“这个方法完成什么业务动作”。

示例：

```go
// 调用 AuthStore 保存验证码，后续登录时用于一次性校验。
if err := s.store.SaveLoginCode(email, code, codeTTL); err != nil {}
```

```js
// 登录成功后探测本地 Agent，用于初始化本地执行环境。
await detectLocalAgent()
```

## 注释边界

- 注释要解释业务目的，不重复代码字面意思。
- 不写“给变量赋值”这种无信息量注释。
- 复杂流程必须在关键分支前写简短说明。
- 公共接口、跨模块调用、持久化读写、外部服务调用必须有注释。

## 提交流程

每完成一个独立功能必须：

1. 更新 `docs/progress.md`。
2. 跑当前能运行的校验。
3. 单独 git commit。
4. commit 只包含当前功能相关文件。

## 数据边界

- 候选人详情、截图、OCR 原文、招聘平台 cookie/profile 不进入云端数据库。
- 云端只保存用户、配置、机器绑定、任务元信息和统计摘要。
- 本地 Agent 文件读写限制在 `agent_data` 目录内。
