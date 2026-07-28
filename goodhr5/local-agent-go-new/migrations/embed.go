// Package migrations 内嵌新本地程序 SQLite 迁移文件，供存储层按版本执行。
package migrations

import "embed"

// Files 保存全部只读 SQLite 迁移文件。
//
//go:embed *.sql
var Files embed.FS
