# GoodHR 5 Go 本地程序重构方案

本文档记录 Go 版本本地程序的长期重构计划。当前 Python 版本继续保留，Go 版本放在 `goodhr5/local-agent-go/`，用于逐步替换本地执行器。

## 目标

- 使用 Go 作为本地程序主进程，负责安装目录、数据目录、端口服务、前端托管、日志、运行组件下载和进程管理。
- 使用 Node Browser Worker 调用 CloakBrowser 官方 Node 能力，负责浏览器启动、关闭、页面操作、截图、下载和 Cookie profile。
- 保持现有业务逻辑不变：平台配置从云端读取，数据尽量存本地，开始任务前仍校验会员。
- 保留 Python 版本作为过渡，不在 Go 版本稳定前删除旧代码。

## 目录规划

```text
goodhr5/local-agent-go/
├── cmd/goodhr-local-agent/      Go 主程序入口
├── internal/app/                HTTP 服务和路由
├── internal/browser/            Node Worker 管理和浏览器 API 转发
├── internal/config/             本地路径、端口和配置
├── internal/process/            进程清理和端口检测
├── internal/response/           统一 JSON 返回
├── internal/runtime/            Node/CloakBrowser 运行组件下载和校验
├── worker-node/                 Node Browser Worker
└── docs/                        重构文档
```

## 阶段

### 第一阶段：Go 主壳

- 启动本地 HTTP 服务，默认优先使用 `127.0.0.1:9001`，被占用时尝试 `9002-9009`。
- 提供 `/health` 和统一 JSON 响应。
- 托管本地前端构建目录。
- 管理本地数据目录。
- 预留运行组件下载接口。

### 第二阶段：Node Worker

- Go 下载 Node runtime 和 Node Worker 包。
- Go 启动、停止、守护 Node Worker。
- Node Worker 使用 CloakBrowser 官方 Node SDK。
- Go 统一清理 Node 和 Chromium 残留进程。

### 第三阶段：浏览器 API

- 迁移当前 Python 版本的浏览器 API：
  - start
  - stop
  - open
  - click
  - type
  - screenshot
  - download
  - cookie/profile
- 下载目录默认使用系统下载目录，同时允许用户设置。

### 第四阶段：本地数据和任务

- 迁移本地任务、岗位、候选人、下载记录、截图记录、AI 配置。
- 本地任务启动时从云端公开接口读取平台配置。
- 任务开始前校验会员。
- 任务日志全部落本地。

### 第五阶段：安装器和更新

- 安装包只内置 Go 主程序和基础前端资源。
- 首次启动下载 Node runtime、Node Worker 和 CloakBrowser。
- 所有下载走 OSS manifest，支持版本号和 sha256 校验。
- 提供“重装运行组件”能力。

## 关键边界

- Go 主程序不直接写死平台选择器。
- Node Worker 只做浏览器控制，不保存业务数据。
- 云端只保存用户、会员和平台配置，不保存候选人详情和 Cookie。
- 浏览器 profile 必须按账号隔离，启动前检查锁文件和残留进程。
- 所有本地 API 错误必须返回中文 `msg`。

## 当前实现状态

- 已创建 Go 版本目录。
- 已开始搭建 Go 主服务、统一响应、运行组件管理和 Node Worker 管理骨架。
- 已接入本地 SQLite 数据库。
- 已完成本地任务、日志、候选人、岗位模板、AI 配置、通用设置、下载记录和截图记录的基础接口。
- 已完成云端平台配置公开读取和会员状态校验客户端。
- 已接入本地任务运行器骨架，开始任务时会先校验会员并读取云端平台配置。
- 已接入 Boss 候选人第一轮扫描和本地候选人保存。
- 已接入本地 AI 打招呼评分，暂不执行真实打招呼点击。
- 已接入受控 Boss 打招呼动作，必须显式传入 `enable_greet=true` 才会点击。
- 已接入任务停止取消、打招呼前随机等待和失败重试。
- 已接入任务后台异步运行和运行状态查询。
- 已接入任务进度、最近日志返回和前端状态查询服务方法。
- 已接入扫描轮数、每轮提取数量、滚动距离等任务运行参数。
