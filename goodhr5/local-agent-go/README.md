# GoodHR 5 Local Agent Go

这是 GoodHR 5 本地程序的 Go 版本目录。当前目录用于长期重构，不影响现有 `goodhr5/local-agent/` Python 版本。

## 当前能力

- Go 主程序可启动本地 HTTP 服务。
- 默认优先监听 `127.0.0.1:9001`，端口被占用时会尝试到 `9009`。
- `/health` 返回统一 JSON。
- `/api/v1/runtime/status` 返回 Node Worker 和 CloakBrowser 运行组件状态。
- `/api/v1/runtime/install` 支持从 manifest 下载 Node runtime、Node Worker 和 CloakBrowser。
- `/api/v1/runtime/install-local-worker` 支持开发阶段安装本地 `worker-node`。
- `/api/v1/console/status` 和 `/api/v1/console/update` 支持检查并更新本地控制台前端包。
- `/api/v1/local/ocr/status` 和 `/api/v1/local/ocr/recognize` 支持本地 OCR 组件状态和图片文字识别。
- 已实现 Node Browser Worker 启动、停止和浏览器 API 转发入口。
- `worker-node/` 已接入 CloakBrowser 官方 Node SDK。
- 已提供基础浏览器 API：打开页面、点击、输入、滚动、提取文本、截图、Cookie、下载记录。
- 已提供本地 SQLite 任务、日志、候选人数据接口，支持简历库分页、筛选、详情和清空。
- 已提供本地岗位模板、AI 配置、通用设置、下载记录和截图记录接口。
- 已提供云端平台配置读取和会员状态校验接口，后续任务启动流程直接复用。
- 已接入本地任务运行器骨架：启动时校验会员、拉取平台配置、写入运行日志和任务状态。
- 已接入 Boss 候选人第一轮扫描：打开云端配置的推荐页，提取可见候选人并保存到本地 SQLite。
- 详情读取支持 DOM、OCR、AI 三种独立模式；选择哪种就只执行哪种，不做隐式兜底。
- 已接入本地 AI 打招呼评分：AI 模式会保存分数和原因。
- 已接入 Boss 打招呼动作，只有启动参数 `enable_greet=true` 时才会真实点击。
- 已接入任务停止信号、打招呼前随机等待和打招呼失败重试。
- 多轮扫描会优先按云端平台配置滚动候选人列表容器，找不到容器时再滚动页面。
- 已接入任务后台异步运行，开始接口会快速返回，状态接口可查询 running。
- 状态接口会返回进度阶段、轮次、任务统计和最近日志，前端服务层已提供查询方法。
- 前端任务列表已接入本地任务进度轮询和进度条展示。
- 任务启动支持配置扫描轮数、每轮提取数量、滚动距离、打招呼等待和重试次数。
- 前端任务列表已接入本地任务运行参数表单。
- 已接入本地浏览器 Profile 元数据接口，平台账号可按账号隔离浏览器目录。
- 浏览器启动、打开页面和任务运行会自动使用本机下载目录，并支持本地设置覆盖下载目录。
- Node Worker 调用失败时会自动尝试重启一次，停止任务时会主动关闭浏览器。
- 截图默认保存到本地数据目录，并自动写入本地截图记录。
- 本地控制台会优先代理 Vite 开发服务 `http://127.0.0.1:5173`，没有开发服务时再使用已更新或仓库内的构建目录。
- 已补齐本地 AI 聊天、岗位默认提示词、岗位要求优化、规则状态等前端本地模式接口。
- 运行组件状态会返回安装进度和已安装版本，便于排查下载卡住或版本不一致。
- 已提供 `/api/v1/diagnostics` 本地诊断接口，可检查端口、目录、运行组件、Worker 和 Profile 锁文件。

## 本地启动

Go 版本本地程序需要 Go 1.25 或以上。SQLite 使用纯 Go 驱动，不需要用户电脑安装 C 编译环境。

```bash
cd goodhr5/local-agent-go
go run ./cmd/goodhr-local-agent
```

启动成功后会自动使用默认浏览器打开 `http://127.0.0.1:端口/admin/`。

关闭自动打开：

```bash
go run ./cmd/goodhr-local-agent --open-console=false
```

指定端口：

```bash
go run ./cmd/goodhr-local-agent --port 19001
```

健康检查：

```bash
curl http://127.0.0.1:19001/health
```

诊断检查：

```bash
curl http://127.0.0.1:19001/api/v1/diagnostics
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

更新控制台前端包：

```bash
curl -X POST http://127.0.0.1:19001/api/v1/console/update \
  -H "Content-Type: application/json" \
  -d '{"manifest_url":"https://oss.58it.cn/goodhr-console-manifest.json"}'
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
  },
  "ocr": {
    "win-x64": {
      "version": "rapidocr-json",
      "url": "https://oss.58it.cn/goodhr-ocr-win-x64.zip",
      "sha256": ""
    },
    "darwin-arm64": {
      "version": "rapidocr-json",
      "url": "https://oss.58it.cn/goodhr-ocr-darwin-arm64.zip",
      "sha256": ""
    }
  }
}
```

OCR 组件是可选运行组件。若使用 RapidOCR-json，压缩包解压后需包含 `RapidOCR-json.exe`、`RapidOCR_json.exe`、`RapidOCR-json` 或 `RapidOCR_json` 之一；也可以通过环境变量 `GOODHR_OCR_EXECUTABLE` 指定可执行文件路径。

控制台前端包 manifest 示例：

```json
{
  "console": {
    "version": "0.1.0",
    "url": "https://oss.58it.cn/goodhr-console.zip",
    "sha256": ""
  }
}
```

## 后续重点

- 将运行组件打包脚本接入正式 OSS 上传流程。
- 继续补齐复杂浏览器 API：更完整的随机人类操作、详情页长截图。
- 完善本地控制台前端包启动时自动检查更新和安装器发布流程。

## 发布运行组件包

编译 Go 本地程序：

```bash
cd goodhr5/local-agent-go
./scripts/build_go_binary.sh
```

交叉编译 Windows x64：

```bash
cd goodhr5/local-agent-go
TARGET_OS=windows TARGET_ARCH=amd64 ./scripts/build_go_binary.sh
```

Windows 本机编译：

```powershell
cd goodhr5/local-agent-go
.\scripts\build_go_binary.ps1 -TargetOS windows -TargetArch amd64
```

Windows 生成安装器需要先安装 Inno Setup 6：

```powershell
cd goodhr5/local-agent-go
.\packaging\build_windows_installer.ps1 -Version "0.1.0"
```

安装器默认安装到当前用户目录，并通过 `--data-dir "{app}\data"` 让本地数据跟随安装目录。

打包 Node Worker 前先确认 `worker-node/node_modules` 已存在。若需要安装依赖，先确认 npm registry 使用国内镜像。

```bash
cd goodhr5/local-agent-go
./scripts/package_worker.sh
```

打包当前系统的 Node runtime：

```bash
cd goodhr5/local-agent-go
./scripts/package_node_runtime.sh
```

脚本会输出 zip 路径和 sha256，可填入 OSS manifest。

## Windows 冒烟测试

Windows 真机启动本地程序后，可在 PowerShell 里执行：

```powershell
cd goodhr5/local-agent-go
.\scripts\windows_smoke_test.ps1 -BaseUrl "http://127.0.0.1:9001"
```

它会检查 `/health`、运行组件状态、Worker 状态和诊断信息。
