// 本文件验证官网下载配置即时更新、停用、坏配置和私有字段隔离。
package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserExtensionReleaseArtifactsMatchDefaults 验证默认配置、迁移、官网文件及包内清单使用同一版本和真实摘要。
func TestBrowserExtensionReleaseArtifactsMatchDefaults(t *testing.T) {
	var release BrowserExtensionRelease
	if err := json.Unmarshal([]byte(defaultBrowserExtensionConfig), &release); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0085_browser_extension_release_config.sql"))
	if err != nil || !strings.Contains(string(migration), defaultBrowserExtensionConfig) {
		t.Fatal("下载迁移和默认配置不同步")
	}
	for _, item := range []struct{ browser, url, digest string }{{"chrome", release.URLChrome, release.SHA256Chrome}, {"firefox", release.URLFirefox, release.SHA256Firefox}} {
		path := filepath.Join("..", "..", "..", "frontend-next", "public", filepath.FromSlash(strings.TrimPrefix(item.url, "/")))
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s 官网文件不存在: %v", item.browser, err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != item.digest {
			t.Fatalf("%s 官网包摘要和下载配置不符", item.browser)
		}
		archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range archive.File {
			if entry.Name != "manifest.json" {
				continue
			}
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := io.ReadAll(io.LimitReader(reader, 64*1024))
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(manifest, &parsed) != nil || parsed.Version != release.Version {
				t.Fatalf("%s 包内版本和后台默认版本不一致", item.browser)
			}
			found = true
		}
		if !found {
			t.Fatalf("%s 包没有 manifest.json", item.browser)
		}
	}
}

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
	if json.Unmarshal([]byte(cfg.ConfigValue), &release) != nil || release.Version != "1.0.2" || len(release.SHA256Chrome) != 64 || len(release.SHA256Firefox) != 64 || release.FirefoxSigned {
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
