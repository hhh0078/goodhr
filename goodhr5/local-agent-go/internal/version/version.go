// Package version 负责提供 GoodHR 本地程序版本信息。
package version

// Value 是当前本地程序版本号。
// 构建正式包时可通过 go build -ldflags "-X goodhr5/local-agent-go/internal/version.Value=版本号" 注入。
var Value = "go-v2-dev"
