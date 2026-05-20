# GoodHR 5 Docker 启动指南

## 快速启动

```bash
cd goodhr5

    docker compose build --no-cache
    docker compose up -d
```

首次启动会自动构建镜像（约 2-3 分钟），后续秒级启动。

| 服务       | 地址                  | 热重载                  |
| ---------- | --------------------- | ----------------------- |
| Go 后端    | http://localhost:8084 | ✅ 改 .go 自动重启      |
| Vue 前端   | http://localhost:5175 | ✅ 改 .ts/.vue 即时刷新 |
| PostgreSQL | localhost:5432        | -                       |
| Redis      | localhost:6379        | -                       |

## 停止

```bash
docker compose down        # 停止并保留数据
docker compose down -v     # 停止并删除数据
```

## 已有 PG/Redis

如果宿主机已运行 PG 和 Redis，注释掉 docker-compose.yml 中 `postgres` 和 `redis` 服务，然后在 `.env` 填写外部地址：

```env
GOODHR_PG_DSN=postgres://postgres:postgres@127.0.0.1:5432/goodhr5?sslmode=disable
GOODHR_REDIS_ADDR=127.0.0.1:6379
```

## Local Agent（本地运行）

```bash
cd local-agent
source .venv/bin/activate
python3 -m app.main
```

Agent 需要 CloakBrowser 桌面环境，不放在容器中。
