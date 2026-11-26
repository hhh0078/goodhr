module goodhr5/cloud/backend

go 1.25.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.12.3
	github.com/redis/go-redis/v9 v9.19.0
	github.com/wechatpay-apiv3/wechatpay-go v0.2.21
	golang.org/x/crypto v0.41.0
	goodhr5/local-agent-go-new v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.48.2 // indirect
)

replace goodhr5/local-agent-go-new => ../../local-agent-go-new
