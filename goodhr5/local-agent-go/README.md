# GoodHR 5 Local Agent Go

这是 GoodHR 5 本地程序的 Go 版本目录。当前目录用于长期重构，不影响现有 `goodhr5/local-agent/` Python 版本。

## 当前能力

- Go 主程序可启动本地 HTTP 服务。
- 默认优先监听 `127.0.0.1:9001`，端口被占用时会尝试到 `9009`。
- `/health` 返回统一 JSON。
- `/api/v1/runtime/status` 返回 Node Worker 和 CloakBrowser 运行组件状态。
- `/api/v1/runtime/install` 支持从 manifest 下载 Node runtime、Node Worker 和 CloakBrowser。
- `/api/v1/runtime/install-local-worker` 支持开发阶段安装本地 `worker-node`。
- 已实现 Node Browser Worker 启动、停止和浏览器 API 转发入口。
- `worker-node/` 已接入 CloakBrowser 官方 Node SDK。
- 已提供基础浏览器 API：打开页面、点击、输入、滚动、提取文本、截图、Cookie、下载记录。
- 已提供本地 SQLite 任务、日志、候选人数据接口。
- 已提供本地岗位模板、AI 配置、通用设置、下载记录和截图记录接口。
- 已提供云端平台配置读取和会员状态校验接口，后续任务启动流程直接复用。
- 已接入本地任务运行器骨架：启动时校验会员、拉取平台配置、写入运行日志和任务状态。
- 已接入 Boss 候选人第一轮扫描：打开云端配置的推荐页，提取可见候选人并保存到本地 SQLite。
- 已接入本地 AI 打招呼评分：AI 模式会保存分数和原因，但当前不会自动点击打招呼。

## 本地启动

Go 版本本地程序需要 Go 1.25 或以上。SQLite 使用纯 Go 驱动，不需要用户电脑安装 C 编译环境。

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

开发阶段安装本地 Worker：

```bash
curl -X POST http://127.0.0.1:19001/api/v1/runtime/install-local-worker
```

安装运行组件：

```bash
curl -X POST http://127.0.0.1:19001/api/v1/runtime/install \
  -H "Content-Type: application/json" \
  -d '{"manifest_url":"https://oss.58it.cn/goodhr-local-runtime-manifest.json"}'
```

manifest 示例：

```json
{
  "node_runtime": {
    "darwin-arm64": {
      "version": "22.0.0",
      "url": "https://oss.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz",
      "sha256": ""
    },
    "win-x64": {
      "version": "22.0.0",
      "url": "https://oss.58it.cn/goodhr-node-runtime-win-x64.zip",
      "sha256": ""
    }
  },
  "node_worker": {
    "darwin-arm64": {
      "version": "0.1.0",
      "url": "https://oss.58it.cn/goodhr-browser-worker-darwin-arm64.zip",
      "sha256": ""
    },
    "win-x64": {
      "version": "0.1.0",
      "url": "https://oss.58it.cn/goodhr-browser-worker-win-x64.zip",
      "sha256": ""
    }
  },
  "cloakbrowser": {
    "darwin-arm64": {
      "version": "146.0.7680.177.5",
      "url": "https://oss.58it.cn/cloakbrowser-darwin-arm64.tar.gz",
      "sha256": ""
    },
    "win-x64": {
      "version": "146.0.7680.177.5",
      "url": "https://oss.58it.cn/cloakbrowser-windows-x64.zip",
      "sha256": ""
    }
  }
}
```

## 后续重点

- 增加 Node runtime 精简包制作脚本。
- 增加 Node Worker 打包脚本。
- 增加运行组件下载进度和版本记录。
- 继续补齐复杂浏览器 API：随机人类操作、截图 OCR、文件下载后缀识别。
- 接入 Boss 打招呼动作，并增加明确的开关保护。
