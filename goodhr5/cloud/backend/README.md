# GoodHR 5 Cloud Backend

## 启动

```bash
CGO_ENABLED=0 go run ./cmd/server
```

默认监听 `:8084`。可通过环境变量修改：

```bash
GOODHR_CLOUD_ADDR=127.0.0.1:8084 CGO_ENABLED=0 go run ./cmd/server
```

## PostgreSQL

未配置 PostgreSQL 时，平台账号映射和任务仍使用内存存储，适合本地纯联调。

配置 `GOODHR_PG_DSN` 后，平台账号映射和任务会写入 PostgreSQL：

```bash
GOODHR_PG_DSN='postgres://postgres:postgres@127.0.0.1:5432/goodhr5?sslmode=disable' \
CGO_ENABLED=0 go run ./cmd/server
```

当前已接入 PostgreSQL 的模块：

```text
AgentStore
AIConfigStore
PlatformAccountStore
PositionStore
TaskStore
TaskLogStore
```

## Redis

默认使用内存存储验证码和会话，适合本地开发。

配置 `GOODHR_REDIS_ADDR` 后启用 Redis：

```bash
GOODHR_REDIS_ADDR=127.0.0.1:6379 \
GOODHR_REDIS_PASSWORD= \
GOODHR_REDIS_DB=0 \
CGO_ENABLED=0 go run ./cmd/server
```

Redis key：

```text
login_code:{email}
session:{token}
```

## 163 SMTP

未配置 SMTP 时，后端使用开发模式 mailer，并在 `send-code` 响应里返回 `debug_code`。

配置 SMTP 后，验证码会通过邮箱发送，响应不再返回 `debug_code`：

```bash
GOODHR_SMTP_HOST=smtp.163.com \
GOODHR_SMTP_PORT=465 \
GOODHR_SMTP_USERNAME=your_email@163.com \
GOODHR_SMTP_PASSWORD=your_smtp_authorization_code \
GOODHR_SMTP_FROM=your_email@163.com \
CGO_ENABLED=0 go run ./cmd/server
```

注意：这里的 `GOODHR_SMTP_PASSWORD` 应该使用 163 邮箱 SMTP 授权码，不是邮箱登录密码。

## PostgreSQL schema

初始 schema 位于：

```text
db/migrations/0001_initial_schema.sql
```

回滚脚本位于：

```text
db/migrations/0001_initial_schema.down.sql
```

第一版先提交标准 SQL 迁移文件，迁移执行器后续接入。

## Agent 连接记录

当前已提供：

```http
POST /api/agents/bind
GET /api/agents/current
POST /api/admin/users/unbind-agent
```

`/api/agents/*` 需要普通登录态；`/api/admin/users/unbind-agent` 需要超级管理员登录态。

连接规则：

- 同一账号可以在新设备登录，后登录会让旧登录失效。
- 本地程序启动时会上报机器码、版本和端口，云端只保存为最近连接记录。
- 超级管理员可在用户管理中清理连接记录，方便排查本地程序连接问题。

## AI 配置

当前已提供：

```http
GET /api/config/user-ai
PUT /api/config/user-ai
GET /api/config/effective-ai
GET /api/system/default-prompts
```

所有接口都需要 `Authorization: Bearer <token>`。

AI 连接参数只来自用户 AI 配置；系统只在 `system_configs` 中提供默认提示词：

```text
system_configs.ai.default_prompts
```

第一版使用内存 `AIConfigStore`，后续替换为 PostgreSQL 实现。

## 平台账号映射

当前已提供：

```http
GET /api/platform-accounts
GET /api/platform-accounts?platform_id=boss
POST /api/platform-accounts/create
DELETE /api/platform-accounts/{id}
```

所有接口都需要 `Authorization: Bearer <token>`。

云端只保存平台、显示名和 Local Agent 的 `local_profile_id`，不保存 cookie/profile 原文。

## 岗位配置

当前已提供：

```http
GET /api/positions
POST /api/positions
DELETE /api/positions/{id}
```

所有接口都需要 `Authorization: Bearer <token>`。

岗位配置保存名称、关键词、排除关键词、岗位描述、默认问候语和 AND/OR 匹配方式。

## 任务 API

当前已提供：

```http
POST /api/tasks
GET /api/tasks
GET /api/tasks/{id}
```

所有接口都需要 `Authorization: Bearer <token>`。

任务创建时支持传入 `position_id` 关联岗位模板。

第一版只保存任务元信息和统计摘要，不保存候选人详情。

## 任务日志 API

当前已提供：

```http
GET /api/tasks/{id}/logs
POST /api/tasks/{id}/logs
```

所有接口都需要 `Authorization: Bearer <token>`。

## 开发快速启动

```bash
cp .env.example .env
go run ./cmd/server  # 开发模式(全内存存储)
curl http://127.0.0.1:8084/health
```

## Local Agent

```bash
cd ../local-agent && source .venv/bin/activate && python3 -m app.main
```

## 前端

```bash
cd ../frontend && npm run dev
```

云端日志只保存运行摘要，不保存候选人完整详情。
