<!-- 文件作用说明：向开发者介绍 local-agent-go-new 的重构目标、技术组成和文档阅读顺序。 -->

# GoodHR 新本地程序

`local-agent-go-new` 是按清晰边界重构后的 GoodHR 本地程序。它保留 CloakBrowser 及其反检测增强，由严格 TypeScript Worker 调用浏览器，Go 负责任务流程和平台适配。

## 技术组成

- Go：本地 HTTP 服务、任务流程、平台适配、本地数据、AI/OCR、运行组件和系统能力。
- TypeScript：Browser Worker 和强类型浏览器封装能力。
- CloakBrowser：浏览器运行和反检测增强。
- SQLite：保存本地任务状态、统一步骤日志、候选人动作摘要、自动回复去重摘要和下载结果。

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

启动前检查按顺序覆盖请求、本地目录、登录、岗位、按任务需要检查会员、个人运行配置、平台配置、Profile、冲突、Node、Worker、CloakBrowser、SQLite、AI/OCR 和系统防睡眠。任务运行期间每批候选人和每轮自动回复还会重新检查登录态。

## 本地接口

- `POST /api/v1/tasks/start`：启动主动打招呼或自动回复。
- `POST /api/v1/tasks/stop`：安全停止任务。
- `GET /api/v1/tasks/{task_id}`：读取任务状态。
- `GET /api/v1/runtime/status`：查看 Node 和 Worker 状态。
- `POST /api/v1/runtime/ensure`：启动 Worker。
- `POST /api/v1/runtime/install`：按云端清单异步安装 Node 22+、CloakBrowser 和可选 OCR，支持 SHA256、安全解压和失败回滚。
- `GET /api/v1/diagnostics`：检查目录、端口、运行组件和 Profile 锁。
- `GET|POST /api/v1/app-update/*`：读取程序更新进度并启动安装包更新。
- `POST /api/v1/page/open`：唯一浏览器打开入口，统一启动或复用 Profile、打开页面，并支持 `new_tab=true` 新增标签页和旧版 `new_page=true` 兼容字段。
- `GET /api/v1/browser/status`、`POST /api/v1/browser/stop`：读取或关闭当前浏览器，不提供第二个启动入口。
- `GET /api/v1/downloads`：查看 Worker 监听到的下载成功、失败和处理中记录。
- `GET /api/v1/downloads/history`：查看 SQLite 中已结束的下载历史；旧版 `/api/v1/local/downloads` 路径继续可用。
- `POST /api/v1/downloads/configure`：切换后续下载目录。
- `POST /api/v1/downloads/clear`：清空下载记录，不删除文件。
- `POST /api/v1/files/open|reveal`：打开下载文件或在 Finder 中定位，路径必须是绝对路径，并位于默认目录或 Worker 已成功使用过的下载目录。

Worker 的完整协议见 `contracts/browser-api.md`。

页面打开会优先复用同域名、同目标路径的已有标签页，避免刷新掉用户手动设置的筛选条件；传入 `new_tab=true` 时会始终新增并切换到一个标签页，登录页即使带有回跳参数也不会被误复用。真实滚轮会读取鼠标落点最近的滚动容器状态进行验证，不使用 JS 推动页面滚动。

CloakBrowser 启动默认启用 `humanize`。同一个持久化 Profile 会获得稳定指纹种子；配置代理时默认启用 GeoIP，让时区、语言和 WebRTC 出口信息跟随代理，调用方显式传入的时区、语言或指纹参数仍然优先。

每个 Profile 首次准备时会保留用户原有书签，并在书签栏前面补齐 GoodHR、BOSS直聘、猎聘猎头端、猎聘和智联招聘入口。书签栏会在所有页面显示；这只是手动导航入口，不参与平台自动化流程。

Worker 会监听已有标签页和新标签页的下载事件。Go 每秒同步一次成功或失败终态，保存 SQLite 记录；首次成功时显示十秒下载提示，可直接打开文件或在 Finder 中定位。文件接口会检查真实路径并阻止软链接越过下载目录；切换目录只影响后续下载，清空记录不删除文件。Worker 不反向调用 Go 业务接口。

四个平台都按 `entry.go`、`position.go`、`candidate.go`、`detail.go`、`greet.go`、`followup.go`、`reply.go` 和 `runtime.go` 分责。每个平台目录中的 `config.json` 是带中文属性说明的本地默认模板和能力核对表，随 Go 程序一起编译；运行时以云端平台配置为主，内置模板只补齐云端旧结构缺失的字段。模板中的 `pending_selectors` 表示旧版也没有可确认的配置，自动回复缺少真实页面地址或选择器时会在启动前明确拦截。

本地程序启动后直接打开云端控制台，并附加实际 `local_port`。新版不托管、不下载第二份本地静态控制台。
如果固定端口上已经是一个健康的 GoodHR 本地程序，新进程只会复用该实例、打开现有控制台后退出；不会结束不明端口占用者。
本地 Go 服务默认监听 `127.0.0.1:43129`，启动参数传入其他端口时，控制台仍以 URL 中的实际 `local_port` 为准；前端没有收到端口时也默认探测 `43129`。内部 Browser Worker 继续使用 `39881`。

AI 客户端支持普通 JSON 和 SSE 流式响应，遇到 429、5xx 或临时网络错误最多重试三次；`detail_mode=ai` 会把真实滚轮生成的分段截图交给多模态模型。OCR 使用常驻 JSON 行协议，运行组件压缩包多一层目录时也会递归找到可执行文件。

Go 与 Worker 步骤日志统一写入岗位日志，每个岗位只保留最近 1000 条。主程序和 Worker 文件日志按 10MB 轮转，各保留 3 份历史文件。本地任务、候选人、会话、下载和步骤摘要保留 90 天；OCR/AI 临时截图读取后立即删除，启动时还会清理崩溃遗留截图、组件压缩包和旧更新包。

程序启动时会把上次异常退出遗留的 `running` 任务改成明确失败状态。任务运行期间每 30 秒检测一次时间断层，电脑休眠后恢复且断层超过 2 分钟时会停止当前任务，避免在页面状态已经变化后继续点击。

## 构建与运行

依赖镜像确认后执行：

```bash
./scripts/prepare-runtime.sh
./scripts/build.sh
./bin/goodhr-local-agent
```

`prepare-runtime.sh` 会通过当前锁定的 `cloakbrowser 0.5.2` 下载它自己的增强 Chromium。Go 不会改为普通 Chrome，也不会绕过 CloakBrowser。CloakBrowser 官方的 `146.0.7680.177.5` 当前只提供 Linux x64 和 Windows x64，macOS 官方最新可用增强内核仍是 `145.0.7632.109.2`，不得跨平台混装。

开发环境可以执行：

```bash
./scripts/run-dev.sh
```

本地程序版本号默认是 `6`。需要临时覆盖时执行：

```bash
./scripts/run-dev.sh --version 6.1
```

下载同步在 Browser Worker 第一次启动前会安静等待，不会把正常的未启动状态打印成错误；Worker 曾经连接成功后如果意外断开，仍会记录提醒。

生成正式 macOS 发布包时执行：

```bash
./scripts/package-release.sh 6
```

发布包会包含带版本号的 Go 主程序、Worker 编译产物和 Worker 生产依赖，输出到已忽略提交的 `release/` 目录。运行组件和本地程序更新只接受 HTTPS 地址与完整 SHA256，校验不通过不会安装。

## 核心原则

- 不整体复制旧目录。
- 先固定边界和协议，再迁移能力。
- Node 原子能力不对外暴露。
- Go 只调用 TypeScript 封装能力。
- TypeScript Worker 不包含任何招聘平台逻辑。
- 所有选择器操作使用统一选择器类型。
- 主流程使用平铺步骤，不隐藏深层调用链。
