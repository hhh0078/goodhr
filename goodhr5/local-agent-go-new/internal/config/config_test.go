// Package config 文件作用：验证开发环境的云端 API、前端控制台地址及环境变量覆盖互不干扰。
package config

import "testing"

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
