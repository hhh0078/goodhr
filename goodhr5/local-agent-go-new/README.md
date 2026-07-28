<!-- 文件作用说明：向开发者介绍 local-agent-go-new 的重构目标、技术组成和文档阅读顺序。 -->

# GoodHR 新本地程序

`local-agent-go-new` 是按清晰边界重构后的 GoodHR 本地程序。它保留 CloakBrowser 及其反检测增强，由严格 TypeScript Worker 调用浏览器，Go 负责任务流程和平台适配。

## 技术组成

- Go：本地 HTTP 服务、任务流程、平台适配、本地数据、AI/OCR、运行组件和系统能力。
- TypeScript：Browser Worker 和强类型浏览器封装能力。
- CloakBrowser：浏览器运行和反检测增强。
- SQLite：保存本地任务状态、候选人动作摘要、自动回复去重摘要和下载结果。

## 唯一正式链路

```text
Go 主流程
  -> Go 平台适配
  -> Go Browser Client
  -> TypeScript 封装能力
  -> TypeScript 原子能力
  -> CloakBrowser
```

## 开发前阅读顺序

1. `AGENTS.md`
2. `docs/architecture.md`
3. `docs/directory-rules.md`
4. `docs/browser-worker-design.md`
5. `docs/runtime-flow.md`
6. `docs/migration-plan.md`
7. `docs/legacy-capability-matrix.md`

## 已实现主流程

```text
StartTask
  -> RunPreflightChecks
  -> 获取任务和 Profile 锁
  -> DispatchTaskFlow
      -> GreetingFlow
      -> AutoReplyFlow
  -> 保存最终状态并同步云端摘要
```

启动前检查按顺序覆盖请求、登录、会员、岗位、个人运行配置、平台配置、Profile、冲突、Node、Worker、CloakBrowser、SQLite、AI/OCR 和系统防睡眠。任务运行期间每批候选人和每轮自动回复还会重新检查登录态。

## 本地接口

- `POST /api/v1/tasks/start`：启动主动打招呼或自动回复。
- `POST /api/v1/tasks/stop`：安全停止任务。
- `GET /api/v1/tasks/{task_id}`：读取任务状态。
- `GET /api/v1/runtime/status`：查看 Node 和 Worker 状态。
- `POST /api/v1/runtime/ensure`：启动 Worker。
- `POST /api/v1/runtime/install`：按云端清单异步安装 Node 22+、CloakBrowser 和可选 OCR，支持 SHA256、安全解压和失败回滚。
- `GET /api/v1/diagnostics`：检查目录、端口、运行组件和 Profile 锁。
- `GET|POST /api/v1/app-update/*`：读取程序更新进度并启动安装包更新。
- `GET|POST /api/v1/browser/*`：本地登录和诊断使用的浏览器入口。
- `GET /api/v1/downloads`：查看 Worker 监听到的下载成功、失败和处理中记录。
- `GET /api/v1/downloads/history`：查看 SQLite 中已结束的下载历史；旧版 `/api/v1/local/downloads` 路径继续可用。
- `POST /api/v1/downloads/configure`：切换后续下载目录。
- `POST /api/v1/downloads/clear`：清空下载记录，不删除文件。
- `POST /api/v1/files/open|reveal`：打开下载文件或在 Finder 中定位，路径必须是绝对路径，并位于默认目录或 Worker 已成功使用过的下载目录。

Worker 的完整协议见 `contracts/browser-api.md`。

页面打开会优先复用同域名、同目标路径的已有标签页，避免刷新掉用户手动设置的筛选条件；登录页即使带有回跳参数也不会被误复用。真实滚轮会读取鼠标落点最近的滚动容器状态进行验证，不使用 JS 推动页面滚动。

Worker 会监听已有标签页和新标签页的下载事件。Go 每秒同步一次成功或失败终态，保存 SQLite 记录；首次成功时显示十秒下载提示，可直接打开文件或在 Finder 中定位。文件接口会检查真实路径并阻止软链接越过下载目录；切换目录只影响后续下载，清空记录不删除文件。Worker 不反向调用 Go 业务接口。

四个平台都按 `entry.go`、`position.go`、`candidate.go`、`detail.go`、`greet.go`、`followup.go`、`reply.go` 和 `runtime.go` 分责。每个平台目录中的 `config.json` 是带中文属性说明的本地默认模板和能力核对表；当前运行仍以云端平台配置为准，模板中的 `pending_selectors` 表示旧版也没有可确认的配置。

本地程序启动后直接打开云端控制台，并附加实际 `local_port`。新版不托管、不下载第二份本地静态控制台。
如果固定端口上已经是一个健康的 GoodHR 本地程序，新进程只会复用该实例、打开现有控制台后退出；不会结束不明端口占用者。

AI 客户端支持普通 JSON 和 SSE 流式响应，遇到 429、5xx 或临时网络错误最多重试三次；`detail_mode=ai` 会把真实滚轮生成的分段截图交给多模态模型。OCR 使用常驻 JSON 行协议，运行组件压缩包多一层目录时也会递归找到可执行文件。

## 构建与运行

依赖镜像确认后执行：

```bash
./scripts/prepare-runtime.sh
./scripts/build.sh
./bin/goodhr-local-agent
```

`prepare-runtime.sh` 会通过当前锁定的 `cloakbrowser 0.3.32` 下载它自己的增强 Chromium。Go 不会改为普通 Chrome，也不会绕过 CloakBrowser。

开发环境可以执行：

```bash
./scripts/run-dev.sh
```

## 核心原则

- 不整体复制旧目录。
- 先固定边界和协议，再迁移能力。
- Node 原子能力不对外暴露。
- Go 只调用 TypeScript 封装能力。
- TypeScript Worker 不包含任何招聘平台逻辑。
- 所有选择器操作使用统一选择器类型。
- 主流程使用平铺步骤，不隐藏深层调用链。
