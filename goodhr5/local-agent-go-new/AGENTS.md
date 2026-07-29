<!-- 文件作用说明：定义 local-agent-go-new 的最高优先级开发规范、目录边界、调用方向和交付检查清单。 -->

# GoodHR 新本地程序开发规范

本文件是 `local-agent-go-new` 的唯一权威开发规范。任何 AI 或开发者修改本目录前，必须完整阅读并遵守本文件。

如本文件与同目录其他说明冲突，以本文件为准；如需求不清楚，先向邓云川确认，不得自行扩大范围。

## 1. 项目定位

GoodHR 本地程序由两部分组成：

- Go 主程序：负责本地接口、任务生命周期、公共任务流程、平台适配、AI/OCR 调用、本地数据、运行组件和系统能力。
- TypeScript Browser Worker：负责调用 CloakBrowser，只提供与招聘平台无关的浏览器封装能力。

正式浏览器链路只能有一条：

```text
Go 主流程
  -> Go 平台适配
  -> Go 强类型 Browser Client
  -> TypeScript 封装能力
  -> TypeScript 原子能力
  -> CloakBrowser
```

禁止再增加 Go 直连 CDP、第二个 Node Worker或其他平行浏览器实现。

Go 对控制台只暴露一个浏览器打开入口：

```text
POST /api/v1/page/open
```

该接口统一负责启动或复用浏览器、打开页面和按 `new_tab=true` 新增标签页。`/api/v1/browser/start` 只属于 Go 调用 Worker 的内部协议，不得重新注册为 Go 对外路由。

## 2. 开发前必须执行

1. 先阅读本文件和本次涉及目录的文档。
2. 先搜索是否已有相同或相似方法、类型、接口和错误码。
3. 向用户总结需求理解，等待确认后再写代码。
4. 有任何职责、流程或字段不明确时，必须询问用户。
5. 安装依赖前检查国内镜像配置：
   - Go：`GOPROXY`
   - npm：`npm config get registry`
6. 不创建开发分支，直接在默认分支开发。
7. 完成功能后执行测试、`git add`、中文 `git commit` 和 `git push`。

## 3. 目录职责

```text
local-agent-go-new/
├── cmd/                         Go 程序入口
├── internal/bootstrap/          依赖组装和程序启动
├── internal/api/                本地 HTTP 路由和参数解析
├── internal/flow/               跨平台公共任务流程
├── internal/platform/           招聘平台差异
├── internal/browser/            Go 到 Worker 的类型、客户端和进程管理
├── internal/storage/            SQLite 数据访问
├── internal/integration/        云端、AI、OCR 等外部能力
├── internal/profile/            浏览器账号和 Profile
├── internal/runtime/            Node、Worker、CloakBrowser、OCR 组件管理
├── internal/updater/            本地程序更新
├── internal/system/             文件、端口、进程、防睡眠等系统能力
├── worker/                      TypeScript Browser Worker
├── contracts/                   Go 与 Worker 的协议
├── migrations/                  SQLite 迁移
├── docs/                        架构、流程和迁移文档
├── assets/                      程序静态资源
├── scripts/                     构建和发布脚本
├── packaging/                   安装包配置
└── test/integration/            跨模块集成测试
```

详细放置规则见 `docs/directory-rules.md`。

## 4. 强制依赖方向

允许：

```text
api -> flow
flow -> platform
flow -> storage
flow -> integration
flow -> browser
platform -> browser contract/client
browser client -> TypeScript Worker HTTP API
TypeScript 封装能力 -> TypeScript 原子能力
```

禁止：

- `api` 编写任务业务流程或平台页面逻辑。
- `storage` 调用流程、平台、浏览器或 HTTP handler。
- `platform` 直接操作 SQLite、AI 或云端。
- `worker` 调用 SQLite、AI、云端或 Go 业务接口。
- `worker` 出现 `boss`、`zhaopin`、`liepin`、`hliepin` 等平台名。
- `flow` 硬编码平台 URL、CSS 选择器或平台按钮文案。
- 同层方法通过 A 调 B、B 调 C、C 调 D 隐藏执行流程。

## 5. Go 主流程规则

### 5.1 启动任务

所有任务开始前必须调用统一的启动前检查：

```text
StartTask
  -> RunPreflightChecks
  -> DispatchTaskFlow
```

`RunPreflightChecks` 内部使用有顺序的检查步骤列表，至少覆盖：

1. 请求参数检查。
2. 本地程序状态检查。
3. 云端登录和会员检查。
4. 岗位配置检查。
5. 平台配置检查。
6. Profile 和登录状态检查。
7. Node Worker 检查。
8. CloakBrowser 检查。
9. 本地目录和 SQLite 检查。
10. AI/OCR 检查，仅在当前流程需要时执行。
11. 同账号、同 Profile、同任务并发冲突检查。
12. 系统防睡眠能力检查。

每个检查步骤必须返回明确的步骤名、结果和错误，不得只返回模糊的“启动失败”。

### 5.2 主流程分类

启动前检查通过后，根据任务类型进入独立流程：

- `greeting`：主动打招呼主流程。
- `auto_reply`：自动回复主流程。
- 后续新流程必须新增独立目录或文件，不得塞进已有流程。

公共能力可以复用，但两个主流程不能互相调用。

### 5.3 平铺编排

编排方法必须像步骤清单一样，在一个位置按顺序展示主要动作：

```text
steps = [
  准备浏览器,
  准备平台页面,
  扫描候选人,
  判断候选人,
  执行动作,
  保存结果,
]
```

禁止同一职责层出现：

```text
A -> B -> C -> D
```

要求：

- 顶层编排方法负责顺序。
- 步骤方法只完成一个明确步骤。
- 步骤方法不得偷偷启动另一个完整流程。
- 不可避免的跨层调用不算同层套娃，例如 Go 平台能力调用 Worker 封装能力。
- 日志中必须能看到每个步骤的开始、成功、失败和耗时。

## 6. 平台层规则

平台相关代码只能放在：

```text
internal/platform/{platform}/
```

每个平台按相同文件职责组织：

- `entry.go`：入口页和登录态。
- `position.go`：岗位选择和平台筛选。
- `candidate.go`：候选人列表和字段整理。
- `detail.go`：打开、读取和关闭详情。
- `greet.go`：主动打招呼。
- `reply.go`：自动回复。
- `followup.go`：索要电话、微信、简历等后续动作。
- `runtime.go`：实现平台统一接口，不堆具体实现。
- `helpers.go`：仅放该平台的无状态小工具。

平台层负责：

- 选择使用哪些 Worker 封装能力。
- 从云端平台配置中读取选择器和页面规则。
- 把通用候选人模型转换为平台页面动作。

平台层禁止：

- 硬编码本可由云端配置的选择器。
- 直接调用 TypeScript 原子能力。
- 自己操作数据库或进行 AI 决策。
- 把平台逻辑放进 Worker。

每个平台必须实现同一组完整公共能力：

- 打开登录页、打招呼页、消息页，并分别执行页面初始化。
- 选择岗位、应用基础筛选。
- 返回结构化候选人数组、定位候选人、滚动列表和翻页。
- 打开、提取、清理和关闭候选人详情。
- 打招呼、收藏、不合适和索要候选人信息。
- 扫描未读会话、读取会话和发送回复。

公共平台接口定义能力，平台目录实现动作顺序，云端平台配置提供 URL 和选择器。禁止为了省事把平台差异塞回主流程或 Worker。

## 7. TypeScript Worker 分层

Worker 必须分成两层。

### 7.1 原子能力层

目录：

```text
worker/src/browser/primitives/
```

原子能力只封装 Playwright/CloakBrowser 的最小操作，例如：

- 查询 locator。
- 读取元素边界。
- 鼠标移动。
- 鼠标按下和松开。
- 键盘按键和文本输入。
- 鼠标滚轮。
- 页面截图。
- 读取文本和属性。

强制规则：

- 原子能力不能注册 HTTP 路由。
- 原子能力不能被 Go 调用。
- 原子能力不能包含平台逻辑。
- 原子能力不能自行组合完整操作。
- 原子能力只允许被 `actions` 调用。

### 7.2 封装能力层

目录：

```text
worker/src/browser/actions/
```

Go 只能调用封装能力。

封装能力必须显式组织步骤。例如封装点击：

```text
ClickAction
  1. 调用封装好的 FindAction
  2. 调用封装好的 MoveAction
  3. 调用原子 ClickPrimitive
  4. 验证点击结果
```

封装输入：

```text
InputAction
  1. 调用封装好的 FindAction
  2. 调用封装好的 MoveAction
  3. 调用原子 ClickPrimitive 获取焦点
  4. 按配置清空原内容
  5. 调用原子 InputPrimitive
  6. 验证输入结果
```

封装滚动：

```text
ScrollAction
  1. 查找滚动目标
  2. 读取目标和视口位置
  3. 移动鼠标到安全区域
  4. 调用原子 WheelPrimitive
  5. 读取滚动前后状态并验证
```

禁止使用 `window.scrollBy`、`element.scrollBy`、注入 `scrollIntoView` 或其他 JavaScript 方式推动滚动。

### 7.3 同层调用限制

- 一个封装能力可以平铺调用多个基础封装能力和原子能力。
- 被调用的基础封装能力不得再次隐藏调用另一个完整封装流程。
- `FindAction` 只负责找。
- `MoveAction` 接收已经找到的目标，只负责移动，不得重新查找。
- `ClickAction` 负责按顺序调用查找、移动和原子点击。
- `InputAction` 负责按顺序调用查找、移动、聚焦和原子输入。

## 8. 通用选择器类型

所有基于页面元素的 Worker 封装能力必须接收统一的 `SelectorSpec`，不得为点击、输入、截图分别发明不同选择器结构。

概念结构：

```text
SelectorSpec
├── frames[]                 可选，iframe 层级
├── parents[]                可选，从外到内的父级层级
│   └── SelectorGroup
├── target                   必填，目标元素
│   └── SelectorGroup
├── state                    visible / attached / enabled
├── timeout_ms
└── description              给日志看的中文目标说明

SelectorGroup
├── selectors[]              多个候选选择器，按顺序尝试
├── index                    当前层是列表时选择第几个
├── text                     可选文本约束
├── exact_text               是否精确匹配
└── attributes              可选属性约束
```

规则：

- `selectors` 必须是数组，即使当前只有一个选择器。
- 多个选择器表示降级候选，按顺序尝试。
- `parents` 是层级数组，从最外层父级到最内层父级。
- 每个父级可以有自己的候选选择器和列表序号。
- `target.index` 表示目标匹配多个元素时取第几个。
- 序号必须明确从 0 开始，协议文档和日志中保持一致。
- 找到元素后生成短生命周期的 `element_ref`，同一次动作内后续步骤复用，避免重复查找。
- 跨页面变化或长时间等待后不得继续使用旧 `element_ref`，必须重新查找。

详细定义见 `docs/browser-worker-design.md`。

## 9. 类型规范

### Go

- HTTP 请求、任务快照、平台配置、Worker 请求和返回必须使用结构体。
- 禁止在核心流程和跨模块协议中使用 `map[string]any`。
- 只有读取未知外部 JSON 的最初边界可以临时使用 `json.RawMessage`，验证后立即转换为类型。
- 可选字段必须明确使用指针、零值约定或自定义类型。

### TypeScript

- `tsconfig.json` 必须启用 `strict: true`。
- 禁止显式和隐式 `any`。
- 外部 JSON 从 `unknown` 开始，验证后才能进入业务方法。
- 所有 HTTP action 必须有独立请求类型、返回类型和运行时校验。
- 请求与返回字段使用 `snake_case`，与 Go JSON 保持一致。
- 依赖版本必须锁定并提交 `package-lock.json`。
- 生产环境只运行编译后的 JavaScript，不使用 `ts-node`。

## 10. 错误处理

Node 所有 HTTP action、异步方法和外部边界必须使用统一异常处理。

统一错误至少包含：

- `code`：稳定错误码。
- `message`：给用户或 Go 看的简短中文说明。
- `action`：当前封装能力。
- `step`：失败步骤。
- `trace_id`：整次调用追踪编号。
- `details`：安全的诊断信息。
- `retryable`：是否适合重试。

选择器未找到统一使用：

```text
ELEMENT_NOT_FOUND
```

诊断信息至少记录：

- 目标中文说明。
- 当前页面 URL，去除敏感参数。
- 尝试过的 frame、父级和目标选择器。
- 每一层实际匹配数量。
- 请求的列表序号。
- 元素状态要求。
- 超时时间。
- 失败步骤。

禁止：

- 吞掉异常后返回成功。
- 只写 `操作失败`。
- 把 Cookie、Token、代理密码、完整个人信息写入日志。
- 用字符串判断代替稳定错误码。

## 11. 日志规范

每个封装操作必须记录：

1. 操作开始。
2. 参数摘要。
3. 每个步骤开始。
4. 每个步骤结果。
5. 操作成功或失败。
6. 总耗时。

统一字段：

- `trace_id`
- `action`
- `step`
- `status`
- `duration_ms`
- `page_url`
- `target_description`
- `error_code`

日志必须短、明确、可搜索。重复尝试需要记录当前次数和总次数。

## 12. 文件和方法规范

- 每个新源代码文件头部必须有中文文件作用说明。
- 每个新增方法和函数必须有中文标准注释。
- 一个文件只负责一种明确能力。
- 单个源文件建议不超过 500 行，超过前必须先拆分职责。
- 不创建只有一个实现且没有替换需求的无意义接口。
- 不为“以后可能用”提前创建空抽象。
- 所有空值、空数组、零值、元素不存在、页面关闭、上下文取消和超时必须安全处理。

## 13. 测试要求

至少包含：

- Go 单元测试：流程步骤、平台规则、存储和错误策略。
- TypeScript 单元测试：选择器解析、查找、移动、点击、输入、滚动和错误规范化。
- Worker 协议测试：Go 请求和 TypeScript 接口字段一致。
- 集成冒烟测试：启动 Worker、启动 CloakBrowser、打开页面、查找、点击、输入、真实滚轮、截图、关闭。
- 平台回归测试：Boss、智联、猎聘企业端、猎聘猎头端分别验证。

修复非简单问题时，必须留下一个能防止复发的测试。

## 14. 文案规范

用户可见文案遵循 GoodHR 风格：

- 短、直接、有人味。
- 卑微但靠谱，搞笑但不油腻。
- 失败时说明下一步。
- 不使用“非法参数”“系统异常”“请联系管理员”等冷冰冰表达。

## 15. AI 提交前检查清单

- [ ] 代码是否放在正确目录。
- [ ] 是否先搜索并复用了已有能力。
- [ ] Worker 是否完全不含平台名和平台流程。
- [ ] Go 是否只调用 Worker 封装能力。
- [ ] 原子能力是否没有对外路由。
- [ ] 选择器是否使用统一 `SelectorSpec`。
- [ ] 编排是否在一个方法里平铺展示主要步骤。
- [ ] 是否避免同层 A -> B -> C -> D 套娃。
- [ ] 请求和返回是否强类型。
- [ ] TypeScript 是否无 `any`。
- [ ] 所有异步边界是否统一捕获异常。
- [ ] 选择器找不到时是否返回详细诊断。
- [ ] 点击、输入、滚动是否记录详细步骤日志。
- [ ] 滚动是否只使用真实鼠标滚轮。
- [ ] 新文件和新方法是否有中文注释。
- [ ] 是否处理空值、超时、取消和页面关闭。
- [ ] 是否完成测试。
- [ ] 是否没有泄露 Token、Cookie 和代理密码。
- [ ] 是否按要求提交并推送。
