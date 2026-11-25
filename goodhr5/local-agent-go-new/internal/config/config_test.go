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
	t.Setenv("GOODHR_CLOUD_WS_BASE", "")
	t.Setenv("GOODHR_CONSOLE_URL", "")
	cfg, err := Load(DefaultHost, DefaultPort, t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CloudURL != "http://127.0.0.1:8084" {
		t.Fatalf("CloudURL = %q", cfg.CloudURL)
	}
	if cfg.CloudWSURL != cfg.CloudURL {
		t.Fatalf("开发命令通道应默认沿用API地址，实际=%q", cfg.CloudWSURL)
	}
	if cfg.ConsoleURL != "http://localhost:5173" {
		t.Fatalf("ConsoleURL = %q", cfg.ConsoleURL)
	}
	if cfg.ConsolePageURL() != "http://localhost:5173/admin/" {
		t.Fatalf("ConsolePageURL() = %q", cfg.ConsolePageURL())
	}
}

// TestLoadCloudWSURL 验证正式、开发与私有环境的独立命令通道配置，不改变API和控制台地址。
func TestLoadCloudWSURL(t *testing.T) {
	previousCloud, previousWS := DefaultCloudURL, DefaultCloudWSURL
	defer func() { DefaultCloudURL, DefaultCloudWSURL = previousCloud, previousWS }()
	DefaultCloudURL, DefaultCloudWSURL = "https://goodhr.58it.cn", "https://goodhr5.58it.cn"
	for _, test := range []struct {
		name, api, ws, wantAPI, wantWS string
	}{
		{"官方默认", "", "", "https://goodhr.58it.cn", "https://goodhr5.58it.cn"},
		{"正式域名尾斜线", "https://goodhr.58it.cn/", "", "https://goodhr.58it.cn/", "https://goodhr5.58it.cn"},
		{"显式任务域名", "", "https://tasks.example.test", "https://goodhr.58it.cn", "https://tasks.example.test"},
		{"私有API覆盖", "http://192.168.1.20:8084", "", "http://192.168.1.20:8084", "http://192.168.1.20:8084"},
		{"私有任务域名", "https://private.example.test", "wss://private-tasks.example.test", "https://private.example.test", "wss://private-tasks.example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOODHR_CLOUD_API_BASE", test.api)
			t.Setenv("GOODHR_CLOUD_WS_BASE", test.ws)
			t.Setenv("GOODHR_CONSOLE_URL", "https://console.example.test")
			cfg, err := Load(DefaultHost, DefaultPort, t.TempDir())
			if err != nil || cfg.CloudURL != test.wantAPI || cfg.CloudWSURL != test.wantWS || cfg.ConsoleURL != "https://console.example.test" {
				t.Fatalf("云端地址配置错误：api=%q ws=%q console=%q err=%v", cfg.CloudURL, cfg.CloudWSURL, cfg.ConsoleURL, err)
			}
		})
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
	legacyChromePath := filepath.Join(cfg.ExtensionsDir, "browser-extension")
	nestedPath := filepath.Join(cfg.ExtensionsDir, "nested", "child")
	for _, path := range []string{validPath, invalidPath, legacyChromePath, nestedPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(validPath, "manifest.json"),
		[]byte(`{"name":"测试扩展","version":"1.0.0","manifest_version":3,"content_scripts":[{"matches":["https://example.com/*"],"js":["content.js"]}]}`),
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
		filepath.Join(legacyChromePath, "manifest.json"),
		[]byte(`{"name":"旧版 Chrome 扩展","version":"1.0.0","manifest_version":3,"background":{"service_worker":"background.js"}}`),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(nestedPath, "manifest.json"),
		[]byte(`{"name":"嵌套扩展","version":"1.0.0","manifest_version":3,"background":{"scripts":["background.js"]}}`),
		0o644,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if paths := cfg.ExtensionPaths(); !reflect.DeepEqual(paths, []string{validPath}) {
		t.Fatalf("ExtensionPaths() = %#v", paths)
	}
}
