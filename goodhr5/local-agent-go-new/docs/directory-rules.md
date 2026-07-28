<!-- 文件作用说明：逐项规定新本地程序每个目录允许和禁止承载的功能。 -->

# 目录放置规则

## `cmd/goodhr-local-agent`

允许：

- 解析启动参数。
- 加载配置。
- 调用 bootstrap。
- 设置退出码。

禁止：

- 注册业务路由。
- 操作数据库。
- 编写任务流程。
- 调用 Browser Worker。

## `internal/bootstrap`

允许：

- 创建数据库、客户端、Runner、Worker Manager 和 HTTP Server。
- 处理程序启动和优雅退出。

禁止：

- 编写具体业务判断。
- 编写平台操作。

## `internal/api`

允许：

- 注册路由。
- 解析和验证 HTTP 参数。
- 调用 flow、runtime、updater 等应用服务。
- 统一 HTTP 响应。

禁止：

- 直接操作招聘页面。
- 编写候选人筛选流程。
- 在 handler 中拼装长业务链。

建议按接口域拆分：

```text
health.go
diagnostics.go
position.go
profile.go
browser.go
runtime.go
update.go
files.go
```

## `internal/flow`

允许：

- 启动前检查。
- 任务生命周期。
- 主动打招呼流程。
- 自动回复流程。
- 候选人判断。
- AI/OCR 调度。
- 结果持久化和统计同步。

禁止：

- CSS 选择器。
- 平台按钮文案。
- Playwright/CDP 调用。

建议结构：

```text
flow/
├── preflight/
├── greeting/
├── auto_reply/
├── decision/
├── lifecycle/
└── shared/
```

## `internal/platform`

允许：

- 平台入口、岗位选择、候选人、详情、打招呼和回复差异。
- 从云端平台配置读取选择器。
- 调用 Go Browser Client 的封装能力。

禁止：

- 直接调用 Worker 原子能力。
- 操作数据库。
- 进行 AI 决策。

每个平台使用相同文件布局，不能把所有方法重新放回一个 `runtime.go`。

## `internal/browser`

### `contract`

- Go 端 Worker 请求、响应、错误和选择器类型。
- 不包含 HTTP 调用。

### `client`

- 调用 Worker HTTP API。
- 超时、重试、协议版本和错误转换。
- 不包含平台逻辑。

### `process`

- Node Worker 启动、停止、健康检查、日志和进程树清理。
- 不包含页面操作。

禁止在 `internal/browser` 增加 Go CDP、Playwright-Go 或平台选择器。

## `internal/storage`

允许：

- SQLite 初始化和迁移。
- Repository。
- 事务。
- 数据模型和查询。

禁止：

- HTTP。
- 浏览器。
- AI。
- 平台流程。

每张表和每个字段必须有中文说明；迁移文件必须说明目的和影响。

## `internal/integration`

按外部能力拆分：

- `cloud`：云端 API。
- `ai`：本地或外部 AI。
- `ocr`：OCR 进程和识别。
- 后续新增外部系统时单独建目录。

禁止把外部客户端混入 flow 或 api。

## `internal/profile`

- Profile 元数据。
- Profile 路径。
- 同账号隔离。
- 登录状态和占用锁。

不直接控制浏览器页面。

## `internal/runtime`

- Node Runtime。
- Browser Worker。
- CloakBrowser。
- OCR 运行组件。
- 下载、校验、解压、版本和安装状态。

不负责任务流程和程序自身更新。

## `internal/updater`

- 本地程序更新。
- 本地控制台更新。
- 更新包校验和启动替换程序。

## `internal/system`

按系统能力拆分：

- `files`：打开文件和在 Finder/资源管理器中显示。
- `ports`：端口监听和占用检查。
- `process`：通用进程结束。
- `power`：防睡眠。
- `notification`：本地声音和系统提示。

## `worker/src/http`

- Worker 路由。
- 请求校验。
- 响应和错误中间件。
- Trace ID。

只能调用 `browser/actions`。

## `worker/src/browser/actions`

- 对 Go 暴露的浏览器封装能力。
- 平铺组合查找、移动、原子输入等步骤。
- 记录完整操作日志。
- 返回强类型结果。

不能出现招聘平台名。

## `worker/src/browser/primitives`

- 最小 Playwright/CloakBrowser 调用。
- 不注册路由。
- 不被 Go 调用。
- 不组合业务动作。

## `worker/src/browser/session`

- Browser、Context、Page、Profile 生命周期。
- 页面关闭事件。
- 标签页切换。
- 会话互斥。

## `worker/src/contracts`

- TypeScript 请求、响应、选择器、错误和事件类型。
- 与 `contracts/browser-api.md` 保持一致。

## `contracts`

- Go 与 Worker 的唯一协议说明。
- 路由、版本、请求、响应、错误码和兼容规则。

## `docs`

- 只放跨模块说明。
- 单个函数的细节放在代码注释和测试，不在文档重复维护。

## `test/integration`

- Go + Worker + CloakBrowser 的跨模块冒烟测试。
- 不存真实 Cookie、账号、手机号和候选人数据。
