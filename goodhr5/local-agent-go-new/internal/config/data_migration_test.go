// Package config 文件作用：验证数据迁移完整性、已有数据保护和路径边界。
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMigrateDataPreservesFiles 验证首次升级完整迁移登录数据和数据库，校验后释放旧目录。
func TestMigrateDataPreservesFiles(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "old"), filepath.Join(root, "data")
	for _, directory := range []string{filepath.Join(source, "profiles", "account"), filepath.Join(destination, "runtime")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"goodhr-local.db": "database", "profiles/account/cookies.sqlite": "login cookies"}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(source, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateData(source, destination); err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		got, err := os.ReadFile(filepath.Join(destination, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", path, got, err)
		}
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("旧目录未移除：%v", err)
	}
	if err := migrateData(source, destination); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateDataProtectsExistingDestination 验证已有新数据时不覆盖、不删除旧数据。
func TestMigrateDataProtectsExistingDestination(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "old"), filepath.Join(root, "data")
	for _, dir := range []string{source, destination} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "goodhr-local.db"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateData(source, destination); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{source, destination} {
		got, err := os.ReadFile(filepath.Join(dir, "goodhr-local.db"))
		if err != nil || string(got) != dir {
			t.Fatalf("已有文件被改动：%q %v", got, err)
		}
	}
}

// TestMigrateDataRejectsNestedPaths 验证配置错误不会把新数据复制到旧目录内部后删掉。
func TestMigrateDataRejectsNestedPaths(t *testing.T) {
	root := t.TempDir()
	for _, pair := range [][2]string{{root, root}, {root, filepath.Join(root, "data")}, {filepath.Join(root, "old"), root}} {
		if err := migrateData(pair[0], pair[1]); err == nil {
			t.Fatalf("未拒绝嵌套路径：%v", pair)
		}
	}
}

// TestWindowsDataLocationAndOverrides 验证安装目录默认值、显式路径优先级和下载目录归属。
func TestWindowsDataLocationAndOverrides(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 安装路径")
	}
	t.Setenv("GOODHR_DATA_DIR", "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveDataDir("")
	if err != nil || got != filepath.Join(filepath.Dir(executable), "data") {
		t.Fatalf("默认目录：%s %v", got, err)
	}
	custom := t.TempDir()
	t.Setenv("GOODHR_DATA_DIR", filepath.Join(custom, "env"))
	cfg, err := Load("", 0, filepath.Join(custom, "argument"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LegacyDataDir != "" || !strings.HasPrefix(cfg.DownloadsDir, cfg.DataDir+string(filepath.Separator)) {
		t.Fatalf("自定义配置：%+v", cfg)
	}
}

// TestMigrationRejectsLinksWithoutDeletingSource 验证链接文件触发保护，原数据库始终保留。
func TestMigrationRejectsLinksWithoutDeletingSource(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "old"), filepath.Join(root, "data")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "goodhr-local.db"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "goodhr-local.db"), filepath.Join(source, "link")); err != nil {
		t.Skipf("无法创建符号链接：%v", err)
	}
	if err := migrateData(source, destination); err == nil {
		t.Fatal("不能迁移链接")
	}
	if content, err := os.ReadFile(filepath.Join(source, "goodhr-local.db")); err != nil || string(content) != "keep" {
		t.Fatalf("原文件丢失：%s %v", content, err)
	}
}

// TestPrepareDataKeepsRuntimeFilesOnDataDisk 验证配置准备后临时目录、下载和数据库都在指定数据盘。
func TestPrepareDataKeepsRuntimeFilesOnDataDisk(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows 临时目录")
	}
	root := t.TempDir()
	for _, key := range []string{"TEMP", "TMP", "TMPDIR", "XDG_CACHE_HOME"} {
		t.Setenv(key, os.Getenv(key))
	}
	cfg, err := Load("", 0, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.PrepareData(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{os.TempDir(), os.Getenv("XDG_CACHE_HOME"), cfg.DatabasePath, cfg.RuntimeDir, cfg.ProfilesDir, cfg.DownloadsDir} {
		if !strings.HasPrefix(path, root+string(filepath.Separator)) {
			t.Fatalf("目录逃回系统盘：%s", path)
		}
	}
}
