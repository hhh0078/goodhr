<!-- 文件作用说明：向开发者介绍 local-agent-go-new 的重构目标、技术组成和文档阅读顺序。 -->

# GoodHR 新本地程序

`local-agent-go-new` 是按清晰边界重构后的 GoodHR 本地程序。它保留 CloakBrowser 及其反检测增强，由严格 TypeScript Worker 调用浏览器，Go 负责任务流程和平台适配。

## 技术组成

- Go：本地 HTTP 服务、任务流程、平台适配、本地数据、AI/OCR、运行组件和系统能力。
- TypeScript：Browser Worker 和强类型浏览器封装能力。
- CloakBrowser：浏览器运行和反检测增强。
- SQLite：只保存本地任务状态、候选人动作摘要和自动回复去重摘要。

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

启动前检查按顺序覆盖请求、登录、会员、岗位、平台配置、Profile、冲突、Node、Worker、CloakBrowser、SQLite、AI/OCR 和系统防睡眠。

## 本地接口

- `POST /api/v1/tasks/start`：启动主动打招呼或自动回复。
- `POST /api/v1/tasks/stop`：安全停止任务。
- `GET /api/v1/tasks/{task_id}`：读取任务状态。
- `GET /api/v1/runtime/status`：查看 Node 和 Worker 状态。
- `POST /api/v1/runtime/ensure`：启动 Worker。
- `GET|POST /api/v1/browser/*`：本地登录和诊断使用的浏览器入口。

Worker 的完整协议见 `contracts/browser-api.md`。

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
