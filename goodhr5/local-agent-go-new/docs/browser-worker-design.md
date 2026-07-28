<!-- 文件作用说明：定义 TypeScript Browser Worker 的原子能力、封装能力、通用选择器、错误和日志设计。 -->

# Browser Worker 设计

## 1. 目标

Worker 对 Go 提供稳定、强类型、与平台无关的浏览器能力。

公共点击、输入、滚动等能力只实现一次。任何平台需要改变点击方式时，应优先修改通用封装能力或通过参数控制，不能在平台目录复制一份点击代码。

## 2. 分层

```text
HTTP Router
  -> Request Validator
  -> Action Orchestrator
      -> FindAction
      -> MoveAction
      -> Primitive
  -> Response Mapper
```

### Router

- 创建或接收 `trace_id`。
- 校验请求。
- 调用一个 Action。
- 统一返回成功或错误。

### Action

- 对 Go 可见。
- 完成一个用户能理解的操作。
- 在同一个方法里按顺序列出主要步骤。
- 捕获并包装所有异常。
- 记录步骤日志。

### Primitive

- 对 Go 不可见。
- 对 Router 不可见。
- 只允许 Action 调用。
- 是最小的 Playwright/CloakBrowser 操作。

## 3. 通用选择器

建议的 TypeScript 概念类型：

```ts
interface SelectorSpec {
  frames?: SelectorGroup[];
  parents?: SelectorGroup[];
  target: SelectorGroup;
  state?: "attached" | "visible" | "enabled";
  timeout_ms?: number;
  description: string;
}

interface SelectorGroup {
  selectors: SelectorCandidate[];
  index?: number;
  text?: string;
  exact_text?: boolean;
  attributes?: Record<string, string>;
}

interface SelectorCandidate {
  type: "css" | "role" | "text" | "test_id";
  value: string;
  name?: string;
}
```

这只是协议设计示意，正式实现前需要同时建立 Go 对应结构体和运行时校验。

### 查找顺序

1. 进入指定 iframe 层级。
2. 从外到内解析父级层级。
3. 每个层级按 `selectors` 顺序尝试。
4. 当前层匹配多个元素时使用 `index`。
5. 在最终父级作用域内查找目标。
6. 验证文本、属性和状态。
7. 生成本次操作使用的 `element_ref`。

单元素和列表查找都必须在 `timeout_ms` 内轮询。列表最终超时时只返回最后一轮完整尝试记录，避免诊断被重复轮询日志淹没。

### 序号规则

- 所有 `index` 从 0 开始。
- 未提供 `index` 时，只有一个匹配则使用该元素。
- 未提供 `index` 但匹配多个时，由 Action 参数决定取第一个还是返回 `ELEMENT_AMBIGUOUS`。
- 涉及点击、输入等写操作时，默认不允许悄悄取第一个，应返回歧义错误。

## 4. 查找能力

`FindAction` 返回：

- `element_ref`
- 实际命中的选择器。
- 每层匹配数量。
- 元素边界。
- 可见、启用和视口状态。
- 当前页面编号和 URL 摘要。

`element_ref` 规则：

- 只在当前 Page 和短时间操作内有效。
- 页面跳转、刷新、关闭、DOM 大幅变化后失效。
- 点击和输入封装能力内部可以复用。
- Go 不应长期保存后跨步骤复用。

## 5. 点击能力

`ClickAction` 必须平铺执行：

1. 校验请求。
2. 调用 `FindAction`。
3. 判断元素是否可见和可点击。
4. 如不在视口，调用封装好的真实滚轮能力使其进入视口。
5. 调用 `MoveAction` 移动到元素安全随机点。
6. 调用原子鼠标按下。
7. 按随机范围等待。
8. 调用原子鼠标松开。
9. 按配置验证 URL、元素状态、页面变化或目标消失。

`MoveAction` 接收查找结果，不得重新执行查找。

## 6. 输入能力

`InputAction` 必须平铺执行：

1. 校验请求和文本。
2. 调用 `FindAction`。
3. 必要时真实滚轮进入视口。
4. 调用 `MoveAction`。
5. 调用原子点击获取焦点。
6. 根据配置执行全选和删除。
7. 调用原子键盘输入，支持字符间随机延迟。
8. 读取输入框值并验证。

禁止直接通过 JavaScript 设置 `value`。

## 7. 滚动能力

滚动必须：

- 使用鼠标移动和滚轮事件。
- 在滚动前读取目标、容器和视口状态。
- 把鼠标移动到安全的滚动区域。
- 分段滚动，允许随机距离和停顿。
- 滚动后读取状态并验证变化。
- 提供 `wheel_anchor` 时，验证鼠标落点元素或其最近可滚动父级的 `scrollTop`、`scrollHeight` 和视口尺寸；不能只看页面 `window.scrollY`。

禁止：

- `window.scrollBy`
- `element.scrollBy`
- `scrollIntoView`
- 通过 `evaluate` 修改 `scrollTop`

## 8. 错误结构

```ts
interface WorkerError {
  code: string;
  message: string;
  action: string;
  step: string;
  trace_id: string;
  retryable: boolean;
  details?: Record<string, unknown>;
}
```

基础错误码：

- `INVALID_REQUEST`
- `BROWSER_NOT_RUNNING`
- `PAGE_NOT_AVAILABLE`
- `PAGE_CLOSED`
- `ELEMENT_NOT_FOUND`
- `ELEMENT_AMBIGUOUS`
- `ELEMENT_NOT_VISIBLE`
- `ELEMENT_NOT_ENABLED`
- `ELEMENT_REF_EXPIRED`
- `MOVE_FAILED`
- `CLICK_FAILED`
- `INPUT_FAILED`
- `SCROLL_FAILED`
- `SCREENSHOT_FAILED`
- `DOWNLOAD_FAILED`
- `ACTION_TIMEOUT`
- `ACTION_CANCELLED`
- `INTERNAL_ERROR`

错误码稳定，中文提示可以优化但不能随意改变错误码含义。

## 9. 统一错误处理

- Router 有最后一道错误中间件。
- 每个 Action 使用 `try/catch` 增加 action 和 step。
- Primitive 把 Playwright 异常转成内部类型后继续抛出，不能吞掉。
- 超时和取消必须区分。
- 失败时可以按安全策略截图，但截图路径不能包含敏感内容。

## 10. 日志示例

```text
trace_id=abc action=click step=find status=start target=打招呼按钮
trace_id=abc action=click step=find status=success matches=1 duration_ms=82
trace_id=abc action=click step=move status=success duration_ms=146
trace_id=abc action=click step=mouse_click status=success duration_ms=91
trace_id=abc action=click status=success duration_ms=337
```

选择器找不到：

```text
trace_id=abc action=click step=find status=failed
error_code=ELEMENT_NOT_FOUND
target=打招呼按钮
parent_level=2
attempted_selectors=3
timeout_ms=5000
```

日志不得输出完整页面 HTML、Cookie、Token、代理密码或候选人完整简历。

## 11. 对外封装能力初始清单

会话：

- `browser.start`
- `browser.stop`
- `browser.status`
- `page.open`
- `page.list`
- `page.use`
- `page.close`

元素：

- `element.find`
- `element.find_all`
- `element.read`
- `element.ensure_visible`

交互：

- `element.click`
- `element.input`
- `keyboard.press`
- `page.scroll`
- `element.scroll`

文件和页面：

- `page.screenshot`
- `element.screenshot`
- `element.screenshot_long`
- `cookie.list`
- `cookie.set`
- `download.configure`
- `download.list`
- `download.clear`
- `overlay.show`
- `overlay.close`

清单之外的能力必须先判断：

1. 是否可由已有封装能力组合。
2. 是否为通用浏览器能力。
3. 是否夹带平台业务。

平台专用能力不得加入 Worker。

长截图仍属于通用浏览器能力：它只接收 `SelectorSpec`，通过鼠标移动和真实滚轮分段截图，不知道候选人或招聘平台名称。Go 负责决定何时调用以及如何把分段交给 OCR。
