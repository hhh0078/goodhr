# GoodHR 5 Local Agent

本地 Agent 运行在用户电脑上，后续负责：

- CloakBrowser 控制。
- 招聘平台页面查找、滚动和点击。
- 当前可见候选人提取。
- 详情弹框截图。
- OCR。
- 本地任务 JSON、截图和 OCR 文件管理。

当前版本只提供 `/health`，用于云端页面探测。

## 启动

```bash
python3 -m app.main
```

默认监听 `127.0.0.1:9001`。后续会改成 `9001-9009` 自动尝试。
