<!-- 文件作用说明：记录 Go 与 TypeScript Browser Worker 的唯一 HTTP 协议和兼容规则。 -->

# Browser Worker API v1

Worker 只监听 `127.0.0.1`。请求和响应字段统一使用 `snake_case`，Go 只能通过强类型 Client 调用本清单中的封装能力。

本清单是 Go 与 Worker 的内部协议，因此仍按职责保留 `browser.start` 和 `page.open`。控制台只允许调用 Go 的 `POST /api/v1/page/open`；Go 不再对外注册第二个浏览器启动路由。

## 统一响应

成功：

```json
{"ok": true, "data": {}, "trace_id": "uuid"}
```

失败：

```json
{
  "ok": false,
  "error": {
    "code": "ELEMENT_NOT_FOUND",
    "message": "目标暂时没找到",
    "action": "element.click",
    "step": "find",
    "trace_id": "uuid",
    "retryable": true,
    "details": {}
  },
  "trace_id": "uuid"
}
```

Go 可以通过 `X-Trace-ID` 传入任务追踪编号；未传时 Worker 自动生成。

## 路由

| 方法 | 路径 | 封装能力 |
|---|---|---|
| GET | `/health` | Worker 健康检查 |
| POST | `/api/v1/browser/start` | 启动或复用 Camoufox |
| POST | `/api/v1/browser/stop` | 关闭浏览器 |
| GET | `/api/v1/browser/status` | 浏览器状态 |
| GET | `/api/v1/runtime/status` | Camoufox 增强二进制安装状态 |
| POST | `/api/v1/page/open` | 打开页面 |
| GET | `/api/v1/page/list` | 标签页列表 |
| POST | `/api/v1/page/use` | 切换标签页 |
| POST | `/api/v1/page/close` | 关闭当前标签页 |
| POST | `/api/v1/page/save-current-document` | 使用当前浏览器会话流式保存当前文档页 |
| GET | `/api/v1/page/url` | 当前页面地址 |
| POST | `/api/v1/element/find` | 查找单个元素 |
| POST | `/api/v1/element/find-all` | 查找列表并读取字段 |
| POST | `/api/v1/element/read` | 读取文本、HTML 或属性 |
| POST | `/api/v1/element/click` | 查找、滚动、移动、原子点击、验证 |
| POST | `/api/v1/element/input` | 查找、滚动、移动、聚焦、清空、原子输入、验证 |
| POST | `/api/v1/page/scroll` | 页面真实鼠标滚轮 |
| POST | `/api/v1/element/scroll` | 元素区域真实鼠标滚轮 |
| POST | `/api/v1/keyboard/press` | 通用按键 |
| POST | `/api/v1/page/screenshot` | 页面截图 |
| POST | `/api/v1/element/screenshot` | 元素截图 |
| POST | `/api/v1/element/screenshot-long` | 真实滚轮分段截取长元素 |
| GET | `/api/v1/cookies` | 读取 Cookie |
| POST | `/api/v1/cookies` | 导入 Cookie |
| GET | `/api/v1/downloads` | 下载记录 |
| POST | `/api/v1/downloads/configure` | 切换后续下载保存目录 |
| POST | `/api/v1/downloads/clear` | 清空内存下载记录，不删除文件 |

`GET /api/v1/downloads/history` 和兼容路径 `GET /api/v1/local/downloads` 属于 Go 本地接口，不属于 Worker 协议，用于读取 SQLite 下载终态历史。

`browser.start` 可以携带 `url`、`wait_until`、`timeout_ms` 和 `new_tab`，供 Go 的统一对外接口在一次内部请求中启动或复用浏览器并打开页面。语言与时区由 Worker 兜底为 `zh-CN` / `Asia/Shanghai`；`geoip` 已停用（用户与平台均在中国大陆），传入不生效。

浏览器窗口大小由浏览器自行管理，页面始终跟随实际窗口。旧客户端传入的 `viewport_width`、`viewport_height` 被忽略；Worker 会清除 Camoufox 启动配置中的窗口和页面尺寸覆盖，但保留其他账号指纹与插件配置。

`page.open` 在 `new_tab` 不为 `true` 时，会先检查全部已有标签页。同协议、同域名且页面路径命中目标路径时直接切换并复用，不执行导航，因此不会清掉用户提前设置的网页筛选条件；`new_tab=true` 会始终新增并切换标签页，登录页查询参数即使包含回跳地址也不会误命中。

## SelectorSpec

```json
{
  "frames": [],
  "parents": [
    {
      "selectors": [
        {"type": "css", "value": ".list"},
        {"type": "role", "value": "list"}
      ],
      "index": 0
    }
  ],
  "target": {
    "selectors": [
      {"type": "role", "value": "button", "name": "打招呼"},
      {"type": "text", "value": "打招呼"}
    ],
    "index": 0
  },
  "state": "enabled",
  "timeout_ms": 5000,
  "description": "候选人打招呼按钮"
}
```

- `frames` 和 `parents` 从外到内解析。
- 可选 `frame_fallbacks` 是备用框架路径数组；每条路径从页面根开始，连同父级、目标及状态一起检查，全部路径共用 `timeout_ms`。最多四条备用路径，每条最多六层；空路径代表主页面。
- 定位诊断中的 `frame_path` 为 `0` 时代表主路径，其余序号代表对应备用路径。歧义错误不会通过切换路径隐藏。
- 每层 `selectors` 都是按顺序降级的候选数组。
- 所有 `index` 从 `0` 开始。
- 写操作未提供 `index` 且匹配多个元素时返回 `ELEMENT_AMBIGUOUS`。
- 页面跳转、刷新、关闭或超过有效期后，旧 `element_ref` 自动失效。
- 列表字段可在 `SelectorSpec` 中使用 `read_property=text|html` 或 `read_attribute` 读取属性。
- 单元素和列表查找都按 `timeout_ms` 轮询，超时错误保留最后一轮选择器尝试明细。
- 列表查找支持可选的 `start_index`（默认 0）和 `max_items`，用于分批读取持续追加的列表。返回的 `index` 始终是原列表序号；起点超过已加载内容时返回空数组。

## 真实滚轮验证

- 所有滚动只通过鼠标移动和 `mouse.wheel` 执行。
- 提供 `wheel_anchor` 时，Worker 只把它作为真实鼠标落点；滚动前后通过目标位置或当前视口截图摘要验证变化，不读取页面内部滚动属性。
- 继续加载时可提供 `progress_target` 跟踪已加载的末尾元素，与 `target` 互斥。Worker 始终执行滚轮，并返回 `progress_target_moved`（位置是否变化）和 `progress_target_visible`（元素底边是否进入可见区域）。末尾已进入窗口时优先把真实鼠标放在末尾，避免滚到外层页面。
- Go 负责等待追加内容和判断任务结束：列表仍在移动时继续滚动，抵达末尾后重复等待加载；浏览器断开、滚动停在中间或等待超时都返回错误。
- 读取和操作页面都禁止注入或执行 JavaScript，只能使用标准 Locator、鼠标、键盘、真实滚轮和截图。

## 长截图

`POST /api/v1/element/screenshot-long` 必须提供 `target`、`directory` 和 `filename`，可提供 `wheel_anchor`、`distance`、`max_parts` 和 `wait_ms`。

执行顺序：

1. 查找截图目标和滚轮落点。
2. 移动鼠标到滚轮落点。
3. 使用真实滚轮回到顶部。
4. 分段截图并计算内容哈希。
5. 使用真实滚轮向下推进。
6. 到底或发现重复分段后停止。

返回 `parts[]`、`count` 和 `complete`。未滚动到底会返回 `SCREENSHOT_FAILED`，不能把截断图片当成功。

## 下载监听

- Firefox 使用原生下载管理，已有页面、新标签页和用户手动下载均由浏览器全局 `Downloads.ALL` 观察器覆盖。
- 下载记录状态为 `pending`、`saved` 或 `failed`。
- Firefox 负责文件保存、实际文件名和同名文件处理；Worker 只读取原生状态快照，不另存或重命名。
- `GET /api/v1/downloads` 返回最近 100 条记录、待处理数量和当前目录。
- `downloads.configure` 必须等待浏览器确认目录已生效，已有文件路径保持原样。
- `downloads.clear` 只清空 GoodHR 记录，不删除已经保存的文件，也不清除浏览器原生历史。
- Go 每秒主动读取一次下载终态，用于本地持久化和完成提示；Worker 不回调 Go 业务接口。

## 当前文档页保存

`POST /api/v1/page/save-current-document` 只保存当前已经打开的 `http/https` 标签页，不接受调用方提供其他 URL。请求可提供 `max_bytes`、`allowed_content_types[]`、`suggested_filename` 和 `timeout_ms`，返回结构与普通下载的 `DownloadRecord` 完全一致。

- 每次请求和最多 5 次跳转都按目标 URL 从当前 `BrowserContext` 重新读取 Cookie。
- 使用 Node 标准网络流写入同目录临时 `.part` 文件，不先把整份文档放进内存。
- `Content-Length` 和真实流大小都受 `max_bytes` 限制，总时限到达会立即断流。
- 失败、超限和文件签名不正确时删除临时文件；PDF 必须以 `%PDF-` 开头。
- 成功后使用安全且不覆盖旧文件的名称，并进入现有下载记录。

## 兼容规则

- v1 字段只能追加可选字段，不得改变现有字段含义。
- 删除字段、改变错误码含义或改变必填规则时必须新增协议版本。
- Worker 不提供任何带平台名称的路由。
- Worker 原子能力不属于 HTTP 协议。
