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

// TestBrowserExtensionReleaseFirefoxSigningGate 验证未签名或安装包信息异常时公开接口不会暴露 Firefox 下载地址。
func TestBrowserExtensionReleaseFirefoxSigningGate(t *testing.T) {
	validSHA256 := strings.Repeat("a", 64)
	for _, item := range []struct {
		name       string
		url        string
		sha256     string
		signed     bool
		wantURL    string
		wantSHA256 string
		wantSigned bool
	}{
		{"已签名HTTPS XPI", "https://downloads.example.test/goodhr-firefox.xpi", validSHA256, true, "https://downloads.example.test/goodhr-firefox.xpi", validSHA256, true},
		{"未签名", "https://downloads.example.test/goodhr-firefox.xpi", validSHA256, false, "", "", false},
		{"相对地址", "/downloads/goodhr-firefox.xpi", validSHA256, true, "", "", false},
		{"HTTP地址", "http://downloads.example.test/goodhr-firefox.xpi", validSHA256, true, "", "", false},
		{"非XPI安装包", "https://downloads.example.test/goodhr-firefox.zip", validSHA256, true, "", "", false},
		{"大写摘要", "https://downloads.example.test/goodhr-firefox.xpi", strings.Repeat("A", 64), true, "", "", false},
		{"摘要长度错误", "https://downloads.example.test/goodhr-firefox.xpi", strings.Repeat("a", 63), true, "", "", false},
		{"摘要含非十六进制字符", "https://downloads.example.test/goodhr-firefox.xpi", strings.Repeat("g", 64), true, "", "", false},
	} {
		t.Run(item.name, func(t *testing.T) {
			store := NewMemorySystemConfigStore()
			server := &Server{systemConfigs: store}
			config, err := json.Marshal(BrowserExtensionRelease{
				Version:        "1.2.3",
				VersionFirefox: "1.2.2",
				URLChrome:      "/downloads/chrome.zip",
				SHA256Chrome:   "chrome-summary-is-preserved",
				URLFirefox:     item.url,
				SHA256Firefox:  item.sha256,
				FirefoxSigned:  item.signed,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: string(config), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.GetBrowserExtension(response, httptest.NewRequest(http.MethodGet, "/api/system/browser-extension", nil))
			var result struct {
				Release BrowserExtensionRelease `json:"release"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
				t.Fatalf("读取公开配置失败: %s", response.Body.String())
			}
			if result.Release.URLFirefox != item.wantURL || result.Release.SHA256Firefox != item.wantSHA256 || result.Release.FirefoxSigned != item.wantSigned {
				t.Fatalf("Firefox 门禁结果不正确: %+v", result.Release)
			}
			if !item.wantSigned && result.Release.VersionFirefox != "" {
				t.Fatal("签名门禁失败时不能公开Firefox独立版本")
			}
			if result.Release.URLChrome != "/downloads/chrome.zip" || result.Release.SHA256Chrome != "chrome-summary-is-preserved" {
				t.Fatalf("Firefox 门禁不应改动 Chrome 配置: %+v", result.Release)
			}
		})
	}
}

// TestBrowserExtensionReleaseBrowserVersions 验证旧 Firefox 请求头与新插件参数分别得到已发布浏览器版本，未配置时保持原行为。
func TestBrowserExtensionReleaseBrowserVersions(t *testing.T) {
	for _, item := range []struct {
		name, query, userAgent, firefoxVersion, chromeVersion, want string
		signed                                                      bool
	}{
		{"旧Firefox保持已签名版本", "", "Mozilla/5.0 Gecko/20100101 Firefox/143.0", "1.0.7", "1.0.8", "1.0.7", true},
		{"旧Chrome使用默认最新版本", "", "Mozilla/5.0 Chrome/155.0.0.0 Safari/537.36", "1.0.7", "1.0.8", "1.0.8", true},
		{"新Firefox明确参数", "?browser=firefox", "", "1.0.7", "1.0.8", "1.0.7", true},
		{"明确Chrome参数优先请求头", "?browser=chrome", "Firefox/143.0", "1.0.7", "1.0.6", "1.0.6", true},
		{"官网无浏览器信息使用统一版本", "", "", "1.0.7", "1.0.6", "1.0.8", true},
		{"未知参数使用统一版本", "?browser=unknown", "Firefox/143.0", "1.0.7", "1.0.6", "1.0.8", true},
		{"旧配置Firefox无独立版本", "", "Firefox/143.0", "", "", "1.0.8", true},
		{"旧配置Chrome无独立版本", "?browser=chrome", "", "", "", "1.0.8", true},
		{"未签名不采用Firefox独立版本", "?browser=firefox", "", "1.0.7", "1.0.8", "1.0.8", false},
		{"无效Firefox版本回退", "?browser=firefox", "", "pending", "1.0.8", "1.0.8", true},
		{"无效Chrome版本回退", "?browser=chrome", "", "1.0.7", "1.0", "1.0.8", true},
	} {
		t.Run(item.name, func(t *testing.T) {
			store := NewMemorySystemConfigStore()
			config, err := json.Marshal(BrowserExtensionRelease{Version: "1.0.8", VersionChrome: item.chromeVersion, VersionFirefox: item.firefoxVersion, URLFirefox: "https://example.test/signed-1.0.7.xpi", SHA256Firefox: strings.Repeat("a", 64), FirefoxSigned: item.signed})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: string(config), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			server := &Server{systemConfigs: store}
			mux := http.NewServeMux()
			mux.HandleFunc("/api/system/browser-extension", server.GetBrowserExtension)
			request := httptest.NewRequest(http.MethodGet, "/api/system/browser-extension"+item.query, nil)
			request.Header.Set("User-Agent", item.userAgent)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			var result struct {
				Release BrowserExtensionRelease `json:"release"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Release.Version != item.want {
				t.Fatalf("浏览器发布版本不正确: %s", response.Body.String())
			}
			if !item.signed && result.Release.VersionFirefox != "" {
				t.Fatal("未签名Firefox不得公开独立版本")
			}
		})
	}
}

// TestBrowserExtensionReleaseAgentCompatibility 验证过渡快照仅用于旧本地程序，浏览器参数优先且私有兼容字段不公开。
func TestBrowserExtensionReleaseAgentCompatibility(t *testing.T) {
	for _, item := range []struct {
		name, query, userAgent, compatibilityVersion, wantVersion, wantChrome, wantFirefox string
		hasCompatibility                                                                   bool
	}{
		{"旧程序获得完整旧快照", "", "Go-http-client/1.1", "1.0.7", "1.0.7", "/downloads/chrome-1.0.7.zip", "https://example.test/signed-1.0.7.xpi", true},
		{"官网使用新配置", "", "", "1.0.7", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
		{"Chrome浏览器使用新配置", "", "Mozilla/5.0 Chrome/155.0.0.0", "1.0.7", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
		{"程序明确Chrome优先", "?browser=chrome", "Go-http-client/1.1", "1.0.7", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
		{"程序明确Firefox优先", "?browser=firefox", "Go-http-client/1.1", "1.0.7", "1.0.6", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
		{"空参数也不采用兼容快照", "?browser=", "Go-http-client/1.1", "1.0.7", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
		{"程序没有快照保持原行为", "", "Go-http-client/1.1", "", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", false},
		{"坏快照版本保持原行为", "", "Go-http-client/1.1", "pending", "1.0.8", "/downloads/chrome-1.0.8.zip", "https://example.test/signed-new.xpi", true},
	} {
		t.Run(item.name, func(t *testing.T) {
			store := NewMemorySystemConfigStore()
			config := browserExtensionReleaseConfig{BrowserExtensionRelease: BrowserExtensionRelease{Version: "1.0.8", VersionChrome: "1.0.8", VersionFirefox: "1.0.6", URLChrome: "/downloads/chrome-1.0.8.zip", URLFirefox: "https://example.test/signed-new.xpi", SHA256Firefox: strings.Repeat("a", 64), FirefoxSigned: true}}
			if item.hasCompatibility {
				config.AgentCompatibility = &BrowserExtensionRelease{Version: item.compatibilityVersion, Note: "compatibility-private-marker", URLChrome: "/downloads/chrome-1.0.7.zip", URLFirefox: "https://example.test/signed-1.0.7.xpi", SHA256Firefox: strings.Repeat("b", 64), FirefoxSigned: true}
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: string(encoded), Enabled: true}); err != nil {
				t.Fatal(err)
			}
			server := &Server{systemConfigs: store}
			mux := http.NewServeMux()
			mux.HandleFunc("/api/system/browser-extension", server.GetBrowserExtension)
			request := httptest.NewRequest(http.MethodGet, "/api/system/browser-extension"+item.query, nil)
			request.Header.Set("User-Agent", item.userAgent)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			var result struct {
				Release BrowserExtensionRelease `json:"release"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Release.Version != item.wantVersion || result.Release.URLChrome != item.wantChrome || result.Release.URLFirefox != item.wantFirefox {
				t.Fatalf("程序兼容快照不正确: %s", response.Body.String())
			}
			if strings.Contains(response.Body.String(), "agent_compatibility") {
				t.Fatal("兼容快照字段不能公开")
			}
			if item.wantVersion != "1.0.7" && strings.Contains(response.Body.String(), "compatibility-private-marker") {
				t.Fatal("非兼容请求不能泄露旧快照内容")
			}
		})
	}
}

// TestBrowserExtensionReleaseAgentCompatibilitySafety 验证旧程序兼容快照仍走下载地址和签名门禁。
func TestBrowserExtensionReleaseAgentCompatibilitySafety(t *testing.T) {
	store := NewMemorySystemConfigStore()
	config := browserExtensionReleaseConfig{BrowserExtensionRelease: BrowserExtensionRelease{Version: "1.0.8"}, AgentCompatibility: &BrowserExtensionRelease{Version: "1.0.7", URLChrome: "javascript:alert(1)", URLFirefox: "https://example.test/unsigned.zip", SHA256Firefox: strings.Repeat("a", 64), FirefoxSigned: true}}
	encoded, _ := json.Marshal(config)
	if err := store.Save(SystemConfig{ConfigKey: browserExtensionConfigKey, ConfigValue: string(encoded), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server := &Server{systemConfigs: store}
	request := httptest.NewRequest(http.MethodGet, "/api/system/browser-extension", nil)
	request.Header.Set("User-Agent", "Go-http-client/1.1")
	response := httptest.NewRecorder()
	server.GetBrowserExtension(response, request)
	var result struct {
		Release BrowserExtensionRelease `json:"release"`
	}
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Release.Version != "1.0.7" || result.Release.URLChrome != "" || result.Release.URLFirefox != "" || result.Release.FirefoxSigned {
		t.Fatalf("兼容快照绕过下载门禁: %s", response.Body.String())
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
