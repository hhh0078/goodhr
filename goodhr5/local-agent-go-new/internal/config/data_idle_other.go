//go:build !windows

// Package config 文件作用：其他系统保留原数据路径，不执行 Windows 旧组件检查。
package config

// ensureLegacyDataIdle 为跨平台迁移测试提供相同入口，生产迁移只在 Windows 启用。
func ensureLegacyDataIdle(_ string) error { return nil }

// lockDataMigration 为其他系统测试保留同一入口，自动迁移仅在 Windows 执行。
func lockDataMigration(_ string) (func(), error) { return func() {}, nil }
