# GoodHR 5 Cloud Backend

## 启动

```bash
CGO_ENABLED=0 go run ./cmd/server
```

默认监听 `:8080`。可通过环境变量修改：

```bash
GOODHR_CLOUD_ADDR=127.0.0.1:18080 CGO_ENABLED=0 go run ./cmd/server
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

## Agent 机器绑定

当前已提供：

```http
POST /api/agents/bind
GET /api/agents/current
```

两个接口都需要 `Authorization: Bearer <token>`。

第一版使用内存 `AgentStore`，后续替换为 PostgreSQL 实现。

## AI 配置

当前已提供：

```http
GET /api/config/system-ai
PUT /api/admin/config/system-ai
GET /api/config/user-ai
PUT /api/config/user-ai
GET /api/config/effective-ai
```

所有接口都需要 `Authorization: Bearer <token>`。

配置优先级：

```text
用户 AI 配置 > 系统默认 AI 配置
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

## 任务 API

当前已提供：

```http
POST /api/tasks
GET /api/tasks
GET /api/tasks/{id}
```

所有接口都需要 `Authorization: Bearer <token>`。

第一版只保存任务元信息和统计摘要，不保存候选人详情。

## 任务日志 API

当前已提供：

```http
GET /api/tasks/{id}/logs
POST /api/tasks/{id}/logs
```

所有接口都需要 `Authorization: Bearer <token>`。

云端日志只保存运行摘要，不保存候选人完整详情。
