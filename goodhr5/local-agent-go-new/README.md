<!-- 文件作用说明：向开发者介绍 local-agent-go-new 的重构目标、技术组成和文档阅读顺序。 -->

# GoodHR 新本地程序

`local-agent-go-new` 用于从清晰边界开始重构 GoodHR 本地程序，不直接复制旧项目的混乱结构。

当前阶段只建立规范和架构文档，尚未迁移生产代码。

## 技术组成

- Go：本地 HTTP 服务、任务流程、平台适配、本地数据、AI/OCR、运行组件和系统能力。
- TypeScript：Browser Worker 和强类型浏览器封装能力。
- CloakBrowser：浏览器运行和反检测增强。
- SQLite：本地任务、候选人、日志、设置和文件记录。

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

## 当前原则

- 不整体复制旧目录。
- 先固定边界和协议，再迁移能力。
- Node 原子能力不对外暴露。
- Go 只调用 TypeScript 封装能力。
- TypeScript Worker 不包含任何招聘平台逻辑。
- 所有选择器操作使用统一选择器类型。
- 主流程使用平铺步骤，不隐藏深层调用链。
