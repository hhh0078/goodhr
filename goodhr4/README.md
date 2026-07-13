# goodhr4

`goodhr4` 被拆成两个目录：

- `frontend`: Vue 侧边栏扩展。开发时使用 `yarn run watch` 持续编译。
- `backend`: Go + Redis + PostgreSQL 配置服务。

## frontend

```bash
cd goodhr4/frontend
yarn install
yarn run watch
```

构建产物在 `goodhr4/frontend/dist`，加载为浏览器扩展即可。

默认后端地址在 `frontend/public/config.js`：

```js
API_BASE: "http://127.0.0.1:8787"
```

## backend

```bash
cd goodhr4/backend
docker compose up -d
psql postgres://goodhr4:goodhr4@127.0.0.1:5432/goodhr4 -f migrations/001_init.sql
go run ./cmd/server
```

如果你已经有现成的 PostgreSQL/Redis（不想再启动一套），可以只启动 backend 容器：

```bash
cd goodhr4/backend
docker compose -f docker-compose.backend-only.yml up -d
```

默认连接 `postgres://postgres:123456@host.docker.internal:5432/goodhr4` 和 `host.docker.internal:6379`，按需修改 `docker-compose.backend-only.yml` 里的环境变量。

### API

- `POST /api/v1/account/bind`
- `GET /api/v1/account/:identifier/settings`
- `POST /api/v1/account/:identifier/settings`
- `GET /healthz`

绑定接口只要求 `identifier` 非空。输入邮箱或手机号都会自动注册，不做验证码验证。
