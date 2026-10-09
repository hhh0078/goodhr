// 本文件验证官网下载配置即时更新、停用、坏配置和私有字段隔离。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserExtensionReleaseReflectsConfig 验证公开接口只返回允许的下载字段，且后台变更不被缓存。
func TestBrowserExtensionReleaseReflectsConfig(t *testing.T) {
	store := NewMemorySystemConfigStore()
	server := &Server{systemConfigs: store}
	for _, item := range []struct {
		value           string
		enabled         bool
		version, chrome string
	}{
		{`{"version":"1.2.3","url_chrome":"/downloads/new.zip","private_key":"do-not-publish"}`, true, "1.2.3", "/downloads/new.zip"},
		{`{"version":"1.2.4","url_chrome":"javascript:alert(1)"}`, true, "1.2.4", ""},
		{`{"version":"1.2.4","url_chrome":"/downloads/new.zip"}`, false, "", ""},
		{`not-json`, true, "", ""},
	} {
		if err := store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: item.value, Enabled: item.enabled}); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		server.GetBrowserExtension(response, httptest.NewRequest(http.MethodGet, "/api/system/browser-extension", nil))
		var result struct {
			Release BrowserExtensionRelease `json:"release"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Release.Version != item.version || result.Release.URLChrome != item.chrome {
			t.Fatalf("下载配置不一致: %s", response.Body.String())
		}
		if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || strings.Contains(response.Body.String(), "do-not-publish") {
			t.Fatal("下载缓存或公开字段不安全")
		}
	}
}

// TestBrowserExtensionReleasePostgresMigration 验证下载配置迁移可执行、包摘要正确且重跑不覆盖后台维护。
func TestBrowserExtensionReleasePostgresMigration(t *testing.T) {
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`CREATE TABLE system_configs(config_key TEXT PRIMARY KEY,config_value JSONB NOT NULL,description TEXT NOT NULL DEFAULT '',enabled BOOLEAN NOT NULL DEFAULT true)`); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0085_browser_extension_release_config.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresSystemConfigStore(db)
	cfg, err := store.Get(browserExtensionConfigKey)
	if err != nil {
		t.Fatal(err)
	}
	var release BrowserExtensionRelease
	if json.Unmarshal([]byte(cfg.ConfigValue), &release) != nil || release.Version != "1.0.0" || len(release.SHA256Chrome) != 64 || len(release.SHA256Firefox) != 64 || release.FirefoxSigned {
		t.Fatal("迁移中的开发包信息不完整")
	}
	cfg.ConfigValue = `{"version":"custom-admin-release"}`
	if err = store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	cfg, err = store.Get(browserExtensionConfigKey)
	if err != nil || !strings.Contains(cfg.ConfigValue, "custom-admin-release") {
		t.Fatal("重跑迁移覆盖后台已维护的版本")
	}
}
