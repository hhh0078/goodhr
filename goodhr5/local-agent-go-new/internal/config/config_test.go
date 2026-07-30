// Package config 文件作用：验证开发环境的云端 API、前端控制台地址及环境变量覆盖互不干扰。
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadSeparatesCloudAndConsoleURL 验证开发环境默认 API 和前端地址各自独立。
func TestLoadSeparatesCloudAndConsoleURL(t *testing.T) {
	t.Setenv("GOODHR_CLOUD_API_BASE", "")
	t.Setenv("GOODHR_CONSOLE_URL", "")
	cfg, err := Load(DefaultHost, DefaultPort, t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CloudURL != "http://127.0.0.1:8084" {
		t.Fatalf("CloudURL = %q", cfg.CloudURL)
	}
	if cfg.ConsoleURL != "http://localhost:5173" {
		t.Fatalf("ConsoleURL = %q", cfg.ConsoleURL)
	}
	if cfg.ConsolePageURL() != "http://localhost:5173/admin/" {
		t.Fatalf("ConsolePageURL() = %q", cfg.ConsolePageURL())
	}
}

// TestLoadAllowsIndependentURLOverrides 验证两个环境变量可以分别覆盖 API 和前端地址。
func TestLoadAllowsIndependentURLOverrides(t *testing.T) {
	t.Setenv("GOODHR_CLOUD_API_BASE", "http://127.0.0.1:18084")
	t.Setenv("GOODHR_CONSOLE_URL", "http://localhost:15173/")
	cfg, err := Load(DefaultHost, DefaultPort, t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CloudURL != "http://127.0.0.1:18084" {
		t.Fatalf("CloudURL = %q", cfg.CloudURL)
	}
	if cfg.ConsolePageURL() != "http://localhost:15173/admin/" {
		t.Fatalf("ConsolePageURL() = %q", cfg.ConsolePageURL())
	}
}

// TestExtensionPaths 验证只加载扩展目录下带有效 manifest 的一级子目录。
func TestExtensionPaths(t *testing.T) {
	cfg, err := Load(DefaultHost, DefaultPort, t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	validPath := filepath.Join(cfg.ExtensionsDir, "valid-extension")
	invalidPath := filepath.Join(cfg.ExtensionsDir, "invalid-extension")
	nestedPath := filepath.Join(cfg.ExtensionsDir, "nested", "child")
	for _, path := range []string{validPath, invalidPath, nestedPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(validPath, "manifest.json"),
		[]byte(`{"name":"测试扩展","version":"1.0.0","manifest_version":3}`),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(invalidPath, "manifest.json"),
		[]byte(`{"name":"","version":"1.0.0","manifest_version":3}`),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(nestedPath, "manifest.json"),
		[]byte(`{"name":"嵌套扩展","version":"1.0.0","manifest_version":3}`),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if paths := cfg.ExtensionPaths(); !reflect.DeepEqual(paths, []string{validPath}) {
		t.Fatalf("ExtensionPaths() = %#v", paths)
	}
}
