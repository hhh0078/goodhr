// Package version 文件作用：提供可在构建时注入的 GoodHR 本地程序版本号。
package version

// Value 是当前本地程序版本号，正式构建可通过 go build -ldflags 注入。
var Value = "6"
