# GoodHR 5 Local Agent Go

这是 GoodHR 5 本地程序的 Go 版本目录。当前目录用于长期重构，不影响现有 `goodhr5/local-agent/` Python 版本。

## 当前能力

- Go 主程序可启动本地 HTTP 服务。
- 默认优先监听 `127.0.0.1:9001`，端口被占用时会尝试到 `9009`。
- `/health` 返回统一 JSON。
- `/api/v1/runtime/status` 返回 Node Worker 和 CloakBrowser 运行组件状态。
- 已预留 Node Browser Worker 启动、停止和浏览器 API 转发入口。
- `worker-node/` 已放入 Node Worker 初版代码，后续接 CloakBrowser 官方 Node SDK。

## 本地启动

```bash
cd goodhr5/local-agent-go
go run ./cmd/goodhr-local-agent
```

指定端口：

```bash
go run ./cmd/goodhr-local-agent --port 19001
```

健康检查：

```bash
curl http://127.0.0.1:19001/health
```

## 后续重点

- 接入 OSS manifest 下载 Node runtime。
- 接入 OSS manifest 下载 CloakBrowser。
- 构建并安装 `worker-node` 到本地运行目录。
- 完成浏览器 API 和当前 Python 版本对齐。
- 迁移本地任务、AI 配置、日志、下载记录和截图记录。
