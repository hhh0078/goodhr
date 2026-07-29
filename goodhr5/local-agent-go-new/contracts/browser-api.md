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
| POST | `/api/v1/browser/start` | 启动或复用 CloakBrowser |
| POST | `/api/v1/browser/stop` | 关闭浏览器 |
| GET | `/api/v1/browser/status` | 浏览器状态 |
| GET | `/api/v1/runtime/status` | CloakBrowser 增强二进制安装状态 |
| POST | `/api/v1/page/open` | 打开页面 |
| GET | `/api/v1/page/list` | 标签页列表 |
| POST | `/api/v1/page/use` | 切换标签页 |
| POST | `/api/v1/page/close` | 关闭当前标签页 |
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
| POST | `/api/v1/overlay/show` | 显示通用浮层 |
| POST | `/api/v1/overlay/close` | 关闭通用浮层 |

`GET /api/v1/downloads/history` 和兼容路径 `GET /api/v1/local/downloads` 属于 Go 本地接口，不属于 Worker 协议，用于读取 SQLite 下载终态历史。

`browser.start` 可以携带 `url`、`wait_until`、`timeout_ms` 和 `new_tab`，供 Go 的统一对外接口在一次内部请求中启动或复用浏览器并打开页面。它还支持 `geoip`；未显式设置时，Worker 会在配置代理后自动启用。

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
- 每层 `selectors` 都是按顺序降级的候选数组。
- 所有 `index` 从 `0` 开始。
- 写操作未提供 `index` 且匹配多个元素时返回 `ELEMENT_AMBIGUOUS`。
- 页面跳转、刷新、关闭或超过有效期后，旧 `element_ref` 自动失效。
- 列表字段可在 `SelectorSpec` 中使用 `read_property=text|html` 或 `read_attribute` 读取属性。
- 单元素和列表查找都按 `timeout_ms` 轮询，超时错误保留最后一轮选择器尝试明细。

## 真实滚轮验证

- 所有滚动只通过鼠标移动和 `mouse.wheel` 执行。
- 提供 `wheel_anchor` 时，Worker 只读查找它最近的可滚动父级，并比较滚动前后的状态。
- 读取状态可以使用 `evaluate`，但禁止在页面里修改 `scrollTop`、调用 `scrollBy` 或 `scrollIntoView`。

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

- Browser Context 创建的已有页面和后续新标签页都会注册 `download` 监听。
- 下载记录状态为 `pending`、`saved` 或 `failed`。
- 保存时避免覆盖同名文件，并根据 URL 或文件头补全常见后缀。
- `GET /api/v1/downloads` 返回最近 100 条记录、待处理数量和当前目录。
- `downloads.clear` 只清空内存记录，不删除已经保存的文件。
- Go 每秒主动读取一次下载终态，用于本地持久化和完成提示；Worker 不回调 Go 业务接口。

## 兼容规则

- v1 字段只能追加可选字段，不得改变现有字段含义。
- 删除字段、改变错误码含义或改变必填规则时必须新增协议版本。
- Worker 不提供任何带平台名称的路由。
- Worker 原子能力不属于 HTTP 协议。
