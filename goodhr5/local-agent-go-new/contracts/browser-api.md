<!-- 文件作用说明：记录 Go 与 TypeScript Browser Worker 的唯一 HTTP 协议和兼容规则。 -->

# Browser Worker API v1

Worker 只监听 `127.0.0.1`。请求和响应字段统一使用 `snake_case`，Go 只能通过强类型 Client 调用本清单中的封装能力。

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
| GET | `/api/v1/cookies` | 读取 Cookie |
| POST | `/api/v1/cookies` | 导入 Cookie |
| GET | `/api/v1/downloads` | 下载记录 |
| POST | `/api/v1/overlay/show` | 显示通用浮层 |
| POST | `/api/v1/overlay/close` | 关闭通用浮层 |

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

## 兼容规则

- v1 字段只能追加可选字段，不得改变现有字段含义。
- 删除字段、改变错误码含义或改变必填规则时必须新增协议版本。
- Worker 不提供任何带平台名称的路由。
- Worker 原子能力不属于 HTTP 协议。
