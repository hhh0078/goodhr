# GoodHR 5 Cloud Backend

## 启动

```bash
CGO_ENABLED=0 go run ./cmd/server
```

默认监听 `:8080`。可通过环境变量修改：

```bash
GOODHR_CLOUD_ADDR=127.0.0.1:18080 CGO_ENABLED=0 go run ./cmd/server
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
