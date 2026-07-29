<!-- 文件作用说明：说明新本地程序的整体架构、组件职责、依赖方向和数据边界。 -->

# 新本地程序架构

## 1. 设计目标

- 任何开发者或 AI 能快速判断代码应该放在哪里。
- Go 负责任务和平台，TypeScript 负责浏览器。
- 只有一套正式浏览器控制实现。
- 公共能力改一次，所有平台共同生效。
- 跨 Go 和 TypeScript 的数据全部强类型。
- 日志能还原每次操作的步骤、耗时和失败原因。

## 2. 组件关系

```text
本地控制台
  -> Go HTTP API
  -> Go Task Flow
  -> Go Platform Adapter
  -> Go Browser Client
  -> TypeScript Worker HTTP Router
  -> TypeScript Browser Actions
  -> TypeScript Browser Primitives
  -> CloakBrowser
  -> 招聘平台
```

旁路能力：

```text
Go Task Flow -> SQLite
Go Task Flow -> Cloud Client
Go Task Flow -> Local AI
Go Task Flow -> OCR
Go Task Flow -> System Power Guard
```

## 3. Go 职责

Go 负责：

- 本地 HTTP 服务。
- 启动前检查。
- 任务状态和取消。
- 主动打招呼主流程。
- 自动回复主流程。
- 平台能力选择和组合。
- AI、关键词和规则判断。
- OCR 调度。
- SQLite 持久化。
- 云端配置读取和统计同步。
- 运行中登录检查、个人拟人节奏和失败邮件通知。
- Node Worker 进程管理。
- 本地运行组件、更新、文件和系统能力。

Go 不负责：

- 直接调用 Playwright。
- 直接连接 CDP。
- 实现第二套点击、输入、滚动和截图。
- 在公共主流程里硬编码平台页面结构。

Go 对控制台只注册 `POST /api/v1/page/open` 作为浏览器打开入口。它负责解析 Profile、下载目录、代理和新增标签页参数，再通过强类型 Client 调用 Worker；`browser.start` 和 `page.open` 在 Worker 内仍按生命周期与页面动作分层。

Go 本地服务默认监听 `127.0.0.1:43129`，并在打开云端控制台时通过 `local_port` 传入实际端口。云端控制台优先使用传入或缓存的实际端口，没有收到端口时默认探测 `43129`；Worker 的 `39881` 仅供 Go 内部调用。

## 4. TypeScript Worker 职责

Worker 负责：

- 启动、复用和关闭 CloakBrowser。
- 管理 Browser、Context、Page 和 Profile。
- 命中已有目标标签页时直接复用，保留用户手动筛选状态。
- 查找和验证元素。
- 模拟鼠标移动、点击、键盘输入和真实滚轮。
- 只读检查页面或最近可滚动父级的滚动状态。
- 截图、下载、Cookie 和标签页。
- 通用提示浮层。
- 统一错误和详细操作日志。

Worker 不负责：

- 判断当前平台是什么。
- 识别 Boss、猎聘、智联专用流程。
- 决定是否给候选人打招呼。
- 调用 AI、OCR、SQLite 或云端。
- 保存岗位业务状态。

## 5. 浏览器能力分层

```text
HTTP Router
  -> Action
      -> FindAction
      -> MoveAction
      -> Primitive
```

- Router：解析和验证请求，只调用 Action。
- Action：对 Go 暴露的完整封装能力。
- Primitive：最小 Playwright/CloakBrowser 操作，只允许 Action 调用。

Go 请求中不得出现 Primitive 名称或路由。

## 6. 数据边界

云端保存：

- 用户、会员、平台账号映射。
- 岗位配置。
- 平台配置。
- 运行状态摘要和累计统计。

本地 SQLite 保存：

- 本地任务运行状态。
- 本地日志。
- 不含简历正文的候选人动作摘要。
- 自动回复去重哈希。
- 下载成功或失败的结果记录。

本地 macOS 用户配置目录保存：

- 浏览器 Profile。
- 截图和下载文件。
- Worker 和任务日志。
- Node、CloakBrowser 和 OCR 运行组件。

Worker 内存保存最近 100 条当前会话下载状态。Go 下载同步流程只把 `saved` 和 `failed` 终态写入 SQLite，并用记录编号去重提示；程序重启后不会把历史记录伪装成仍在处理的下载。

下载目录可以在运行时切换。文件打开和 Finder 定位接口只接受绝对路径，并只允许访问默认下载目录或 Worker 已成功使用过的下载目录；校验真实路径，禁止通过软链接越界。切换目录只影响后续下载，清空下载记录不会删除文件。

浏览器 Profile 保存：

- Cookie。
- LocalStorage。
- 平台登录状态。
- 浏览器缓存。
- 用户已有书签，以及幂等补齐的 GoodHR、BOSS、猎聘和智联默认导航书签。

同一个持久化 Profile 使用稳定的 CloakBrowser 指纹种子。配置代理时默认启用 GeoIP；未配置代理时使用调用方设置或中国区默认时区和语言。

禁止把 Cookie、完整简历截图、OCR 原文和敏感页面内容上传到云端日志。

## 7. 并发边界

- 同一个 Profile 同一时间只允许一个会修改页面状态的任务。
- 主动打招呼和自动回复不得同时控制同一个页面。
- Worker 对页面操作串行执行，除非能力明确声明只读且经过验证。
- AI 可以在不操作页面时有限并发。
- 任务停止必须通过 Context 取消，并让当前原子操作尽快返回。

## 8. 正式实现选择

保留：

- Go 主程序。
- TypeScript Node Worker。
- CloakBrowser 官方 Node SDK。

不迁移：

- 旧的 Go 直连 CDP 实验实现。
- Node 中的平台专用路由。
- 超大单文件 Worker。
- 大量 `map[string]any` 和无校验 JSON。

本地控制台不再作为独立静态包安装。Go 服务启动后打开云端控制台，并通过 `local_port` 告知网页当前本地接口端口。
